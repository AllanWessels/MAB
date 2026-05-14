# mab — multi-armed bandit service

A small Go + gRPC service that solves the classical **multi-armed bandit**
problem: given a set of channels (arms), each with an unknown reward
distribution, decide which to pull next so that the cumulative reward is
maximized. The service exposes a single RPC, persists per-experiment state
to DynamoDB, and supports three algorithms selected per-request.

It is built for use cases like:

- Routing notifications across messaging channels (push, email, SMS) and
  letting the algorithm find which one yields the most engagement per user
  segment.
- A/B/n testing where you want adaptive traffic allocation instead of fixed
  splits.
- Dynamic recommendation slot selection.

---

## Table of contents

1. [How it works](#how-it-works)
2. [Algorithms](#algorithms)
3. [API](#api)
4. [Bringing the stack up](#bringing-the-stack-up)
5. [Calling the service](#calling-the-service)
6. [What to expect at runtime](#what-to-expect-at-runtime)
7. [Resetting bandit state](#resetting-bandit-state)
8. [Configuration](#configuration)
9. [Layout](#layout)
10. [Local development without Docker](#local-development-without-docker)
11. [Operational notes](#operational-notes)
12. [Troubleshooting](#troubleshooting)

---

## How it works

The service treats each unique `experiment_id` as an independent bandit. For
every experiment it tracks, per arm (channel):

- `count` — how many times the arm has been pulled
- `sum`   — the sum of rewards observed for that arm

Empirical mean is `sum / count`. The bandit algorithm reads this state to
score arms and pick the next one. State is persisted to DynamoDB on every
write, keyed by `experiment_id`.

A single round trip — `Pull` — combines *update* and *select*:

```
       client                          mab-server                  DynamoDB
         │   Pull(experiment, last_arm,    │                          │
         │        reward, candidates)      │                          │
         ├────────────────────────────────►│                          │
         │                                 │   GetItem(experiment_id) │
         │                                 ├─────────────────────────►│
         │                                 │◄─────────────────────────┤
         │                                 │ state.update(arm,reward) │
         │                                 │ next = algo.select(...)  │
         │                                 │   PutItem(...)           │
         │                                 ├─────────────────────────►│
         │   PullResponse(next_channel)    │                          │
         │◄────────────────────────────────┤                          │
```

Arms are **discovered dynamically**: the first time you mention a channel ID
in `candidate_channels`, it becomes part of that experiment's universe.
There is no startup configuration of arm count.

---

## Algorithms

All three are stateless functions over the persisted `State`. You can switch
algorithms mid-experiment by changing `algo_type` on the next request —
state is shared because it's just counts and sums.

| `algo_type` | params (defaults) | rule | best for |
|---|---|---|---|
| `ALGO_UCB` | `ucb_c` (√2) | argmax `mean + c·√(ln N / n)` — UCB1 | classic, deterministic, handles cold-start well (unseen arms score `+∞`) |
| `ALGO_EPSILON_GREEDY` | `epsilon` (0.1) | with prob `ε` pick uniformly, else greedy mean | simple, easy to reason about; brittle if first samples are unlucky |
| `ALGO_EPSILON_DECAY` | `decay_rate` (1.0) | as above but `ε_t = min(1, k / (N+1))` | fast cold-start (forces exploration early), exploits late |

### Choosing one

- **You want robust convergence with minimal tuning** → `ALGO_UCB`.
- **You want a known, fixed exploration rate** → `ALGO_EPSILON_GREEDY`.
- **You want forced exploration then exploitation** → `ALGO_EPSILON_DECAY`.

### Tie-breaking

When multiple arms have the same score the algorithms break ties by the
**lowest arm ID**. This is deterministic, useful for tests, but means
ε-greedy's cold start can stick on arm 1 if early rewards happen to be 0
(arm 2, 3, ... all tie at mean 0, lowest ID wins until random exploration
breaks the tie). Use `ALGO_EPSILON_DECAY` or `ALGO_UCB` if this matters.

---

## API

Single RPC: `mab.v1.BanditService/Pull(PullRequest) → PullResponse`.

### `PullRequest`

| field | type | required | meaning |
|---|---|---|---|
| `experiment_id` | string | yes | tenant key — state is fully isolated per id |
| `algo_type` | enum | yes | `ALGO_UCB`, `ALGO_EPSILON_GREEDY`, `ALGO_EPSILON_DECAY` |
| `parameters` | `AlgoParams` | no | algo hyperparams; only the relevant field is read |
| `channel_chosen` | int32 | no | arm pulled in the previous round (ignored unless `has_prior=true`) |
| `value_from_channel` | double | no | reward observed for `channel_chosen` |
| `has_prior` | bool | no | if true, update state with `(channel_chosen, value_from_channel)` before selecting |
| `candidate_channels` | repeated int32 | no | restrict selection to these arm IDs; empty = arms already seen |

### `AlgoParams`

| field | algo | default | meaning |
|---|---|---|---|
| `ucb_c` | UCB | √2 | exploration constant in `mean + c·√(ln N / n)` |
| `epsilon` | ε-greedy | 0.1 | probability of uniform random exploration |
| `decay_rate` | ε-decay | 1.0 | `ε_t = min(1, decay_rate / (N+1))` |

### `PullResponse`

| field | type | meaning |
|---|---|---|
| `next_channel_to_use` | int32 | arm to play this round |
| `total_pulls` | int64 | all-time pulls for this experiment |
| `arm_pulls` | int64 | pulls for the *returned* arm |
| `arm_mean` | double | empirical mean reward for the *returned* arm |

### Errors

| gRPC code | when |
|---|---|
| `INVALID_ARGUMENT` | empty `experiment_id` or `algo_type == ALGO_UNSPECIFIED` |
| `FAILED_PRECONDITION` | no `candidate_channels` and the experiment has no observed arms yet |
| `INTERNAL` | DynamoDB load/save failure |

The proto definition lives in `proto/bandit.proto`. The server has gRPC
reflection enabled — any reflection-aware client can introspect the schema.

---

## Bringing the stack up

Requirements: Docker and Docker Compose v2.

```sh
make up          # docker compose up --build -d
make logs        # tail mab-server logs
make down        # stop and remove volumes
```

What you get:

- `mab` container — gRPC server on `localhost:50051`
- `dynamodb` container — `amazon/dynamodb-local` on `localhost:8000`, **in-memory** mode

On first boot the server auto-creates the `bandit_state` table.

Health check: gRPC health service is registered.
```sh
grpcurl -plaintext localhost:50051 list grpc.health.v1.Health
```

---

## Calling the service

### Cold start

```json
{
  "experiment_id": "promo-channels",
  "algo_type": "ALGO_UCB",
  "candidate_channels": [1, 2, 3]
}
```
Response:
```json
{"nextChannelToUse": 1}
```

The very first call has no prior knowledge, so `total_pulls` stays 0. The
algorithm just picks an arm. With UCB and all candidates unseen, every arm
ties at `+∞`, so the lowest ID wins.

### Subsequent calls — the steady-state loop

```text
last_arm = pull(cold_start_request).next_channel_to_use
forever:
    reward = observe(last_arm)            # e.g. did the user click?
    response = pull(
        experiment_id, algo_type, parameters,
        channel_chosen=last_arm,
        value_from_channel=reward,
        has_prior=True,
        candidate_channels=[...],
    )
    last_arm = response.next_channel_to_use
```

Each call updates state before selecting. The `parameters` field can change
per request — e.g. you might decay `ucb_c` yourself by passing a smaller
value as the experiment matures.

### Example with reward feedback

```sh
grpcurl -plaintext -d '{
  "experiment_id": "promo-channels",
  "algo_type": "ALGO_UCB",
  "parameters": {"ucb_c": 1.4},
  "channel_chosen": 2,
  "value_from_channel": 1.0,
  "has_prior": true,
  "candidate_channels": [1, 2, 3]
}' localhost:50051 mab.v1.BanditService/Pull
```

### Adding a new channel mid-experiment

Just include the new ID in `candidate_channels`. The algorithm will see it
has `n=0` and (for UCB) force-explore it on the next pull.

```json
{
  "experiment_id": "promo-channels",
  "algo_type": "ALGO_UCB",
  "candidate_channels": [1, 2, 3, 4]
}
```

---

## What to expect at runtime

### Convergence

These are stochastic algorithms. Over enough rounds they all settle on the
best arm, but the *rate* differs:

| algo | rounds to ~80% optimal (3 arms, Δ=1.0) | notes |
|---|---|---|
| `UCB` | ~30–50 | periodic re-exploration, never stops fully |
| `ε-greedy` (ε=0.1) | unpredictable — 5 to 50+ | sensitive to early reward outcomes |
| `ε-decay` (k=3) | ~10–20 | fast because ε≈1 for first few rounds |

Even after convergence, UCB will sample suboptimal arms periodically — this
is by design, the `√(ln N / n)` term grows for any arm whose `n` stays
small. Regret stays logarithmic, not zero.

### Numerical example

With arm 2 paying 1.0 and arms 1, 3 paying 0.0, UCB over 30 rounds
typically picks arm 2 ~70% of the time. ε-decay (`decay_rate=3`) typically
80–90%. ε-greedy (`ε=0.1`) is highly variable — if the first few rounds
miss arm 2, it can stay stuck for 15+ rounds before random exploration
breaks out.

### Latency

Each `Pull` is one DynamoDB `GetItem` + one `PutItem`. Against
DynamoDB Local on the same machine, end-to-end p50 is sub-millisecond
inside the docker network. In production against real DynamoDB, expect
~5–15 ms p50 depending on region and consistency settings.

---

## Resetting bandit state

State persists in the `bandit_state` table, keyed by `experiment_id`. You
have a few options depending on what you want to reset.

### Reset one experiment

Delete the item for that key:

```sh
docker exec -it mab-dynamodb sh -c '
  aws --endpoint-url=http://localhost:8000 \
      --region=us-east-1 \
      dynamodb delete-item \
      --table-name bandit_state \
      --key "{\"experiment_id\":{\"S\":\"promo-channels\"}}"
'
```

(Requires AWS CLI inside the dynamodb container — `amazon/dynamodb-local`
doesn't ship with it. Easier: install `awscli` locally and run the same
command pointing at `http://localhost:8000`.)

The simplest reset in practice: **use a fresh `experiment_id`**. State is
per-id, so `promo-channels-v2` starts from zero.

### Reset everything (drop the whole table)

```sh
make down        # docker compose down -v  — kills both containers and volumes
make up          # comes back with an empty table
```

Because the compose file runs DynamoDB Local with `-inMemory`, a plain
restart of just the dynamodb container also wipes state:
```sh
docker compose restart dynamodb
```

### Programmatic reset (future work)

There is no `Reset` RPC today. If you want one, the simplest implementation
is a `Reset(experiment_id)` that calls `DeleteItem` on the store. Open an
issue if you'd like this exposed.

---

## Configuration

The server reads these environment variables:

| var | default | meaning |
|---|---|---|
| `GRPC_ADDR` | `:50051` | listen address |
| `DDB_TABLE` | `bandit_state` | DynamoDB table name |
| `DDB_ENDPOINT` | *(empty)* | override DynamoDB endpoint (set to `http://dynamodb:8000` in compose) |
| `AWS_REGION` | `us-east-1` | AWS region |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | `local` / `local` | DynamoDB Local accepts anything but the SDK requires non-empty values |

For real AWS, leave `DDB_ENDPOINT` unset and supply real credentials via the
normal AWS SDK chain (env, EC2/ECS role, etc.).

---

## Layout

```
cmd/server/        gRPC entrypoint, wiring
internal/algo/     algorithms + State (counts/sums per arm)
internal/store/    Store interface, Dynamo + in-memory impls
internal/server/   gRPC handler with per-experiment locking
proto/             schema (bandit.proto)
gen/               protoc output (gitignored)
Dockerfile         multi-stage: protoc gen → go test → go build → distroless
docker-compose.yml mab + dynamodb-local
Makefile           proto / build / test / up / down
```

Generated proto code is intentionally not committed. The Dockerfile
generates it during the proto stage; `make proto` does it locally.

---

## Local development without Docker

You'll need Go 1.22+, `protoc`, and the protoc Go plugins:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
make proto    # generates gen/mabpb/*
make test     # runs algo + server tests against in-memory store
```

Run the server pointed at a local DynamoDB:

```sh
docker run --rm -p 8000:8000 amazon/dynamodb-local
DDB_ENDPOINT=http://localhost:8000 \
AWS_ACCESS_KEY_ID=local AWS_SECRET_ACCESS_KEY=local \
  go run ./cmd/server
```

The in-memory store (`internal/store/memory.go`) is wired in via the
`Store` interface and used by tests — handy if you want to play with the
algorithms without DynamoDB at all.

---

## Operational notes

### Concurrency

Per-experiment pulls are serialized by an in-process mutex map. Different
experiments run in parallel. This is sufficient for **a single replica**.

For multi-replica deployments the mutex is not enough: two replicas can
both `GetItem` then `PutItem` for the same experiment_id, and the later
write wins, losing the other's update. Two paths:

1. Switch the store to **DynamoDB conditional updates** — use `UpdateItem`
   with atomic `ADD` on `total_pulls` and the arm's `count`/`sum`. This
   makes updates lock-free and correct under concurrency; the select step
   still needs a fresh read but no longer races.
2. Front the service with a **sticky routing layer** (consistent hash on
   `experiment_id`) so every experiment lands on one replica.

### Persistence guarantees

`Save` uses `PutItem`, which is single-item atomic in DynamoDB. A crash
between `Update` (in memory) and `Save` (network) loses the update —
acceptable for bandits (one missing reward sample doesn't matter), but if
you need exactly-once accounting, switch to atomic updates as above.

### Production DynamoDB

The table schema is just `experiment_id` (S) as the partition key. The
service auto-creates it with `PAY_PER_REQUEST` billing if missing. In
production you'll likely want to provision the table out-of-band (Terraform
/ CDK / etc.) with provisioned capacity and PITR.

### Observability

Today the server logs only its startup line. Add gRPC interceptors for
request logs, metrics (Prometheus), and tracing (OpenTelemetry) as needed.

---

## Troubleshooting

**`FAILED_PRECONDITION: no candidate or known arms to select from`**
You called `Pull` with no `candidate_channels` and the experiment has
never seen an arm. First call to a new experiment must include candidates.

**`INVALID_ARGUMENT: unknown algo_type ALGO_UNSPECIFIED`**
You omitted `algo_type` or sent the zero value. Set one of `ALGO_UCB`,
`ALGO_EPSILON_GREEDY`, `ALGO_EPSILON_DECAY`.

**ε-greedy is "stuck" on one arm**
Classic cold-start: all unseen arms tie at mean=0, ties go to the lowest
ID. Either switch to UCB / ε-decay, raise `epsilon`, or seed the experiment
with one round per candidate before relying on selection.

**`grpcurl: command not found`** — without installing it locally:
```sh
docker run --rm --network=mab_default fullstorydev/grpcurl:latest \
  -plaintext mab:50051 list
```

**DynamoDB Local data disappears on restart**
Expected — compose runs it with `-inMemory`. To keep state across
restarts, remove the `-inMemory` flag in `docker-compose.yml` and mount a
volume at `/home/dynamodblocal/data`.

**Tests fail in Docker build**
The Dockerfile runs `go test ./...` between `go mod tidy` and `go build`.
Run `--progress=plain` to see test output:
```sh
docker compose build --progress=plain mab
```
