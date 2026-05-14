package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"mab/internal/algo"
)

const (
	attrExperimentID = "experiment_id"
	attrTotalPulls   = "total_pulls"
	attrArms         = "arms"
	attrCount        = "count"
	attrSum          = "sum"
)

type Dynamo struct {
	client *dynamodb.Client
	table  string
}

func NewDynamo(client *dynamodb.Client, table string) *Dynamo {
	return &Dynamo{client: client, table: table}
}

// EnsureTable creates the bandit_state table if it does not exist. Intended
// for dev/Docker Compose against DynamoDB Local; in production the table
// should be provisioned out-of-band.
func (d *Dynamo) EnsureTable(ctx context.Context) error {
	_, err := d.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: &d.table})
	if err == nil {
		return nil
	}
	var nf *types.ResourceNotFoundException
	if !errors.As(err, &nf) {
		return fmt.Errorf("describe table: %w", err)
	}
	_, err = d.client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   &d.table,
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String(attrExperimentID), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String(attrExperimentID), KeyType: types.KeyTypeHash},
		},
	})
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	return nil
}

func (d *Dynamo) Load(ctx context.Context, experimentID string) (*algo.State, error) {
	out, err := d.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      &d.table,
		ConsistentRead: aws.Bool(true),
		Key: map[string]types.AttributeValue{
			attrExperimentID: &types.AttributeValueMemberS{Value: experimentID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	state := algo.NewState()
	if len(out.Item) == 0 {
		return state, nil
	}
	if v, ok := out.Item[attrTotalPulls].(*types.AttributeValueMemberN); ok {
		n, err := strconv.ParseInt(v.Value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse total_pulls: %w", err)
		}
		state.TotalPulls = n
	}
	armsAttr, _ := out.Item[attrArms].(*types.AttributeValueMemberM)
	if armsAttr == nil {
		return state, nil
	}
	for armKey, raw := range armsAttr.Value {
		armID, err := strconv.ParseInt(armKey, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("arm key %q: %w", armKey, err)
		}
		m, ok := raw.(*types.AttributeValueMemberM)
		if !ok {
			continue
		}
		count, err := parseN(m.Value[attrCount])
		if err != nil {
			return nil, fmt.Errorf("arm %d count: %w", armID, err)
		}
		sum, err := parseF(m.Value[attrSum])
		if err != nil {
			return nil, fmt.Errorf("arm %d sum: %w", armID, err)
		}
		state.Counts[int32(armID)] = int64(count)
		state.Sums[int32(armID)] = sum
	}
	return state, nil
}

func (d *Dynamo) Save(ctx context.Context, experimentID string, s *algo.State) error {
	arms := make(map[string]types.AttributeValue, len(s.Counts))
	for arm, count := range s.Counts {
		arms[strconv.FormatInt(int64(arm), 10)] = &types.AttributeValueMemberM{
			Value: map[string]types.AttributeValue{
				attrCount: &types.AttributeValueMemberN{Value: strconv.FormatInt(count, 10)},
				attrSum:   &types.AttributeValueMemberN{Value: strconv.FormatFloat(s.Sums[arm], 'g', -1, 64)},
			},
		}
	}
	_, err := d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &d.table,
		Item: map[string]types.AttributeValue{
			attrExperimentID: &types.AttributeValueMemberS{Value: experimentID},
			attrTotalPulls:   &types.AttributeValueMemberN{Value: strconv.FormatInt(s.TotalPulls, 10)},
			attrArms:         &types.AttributeValueMemberM{Value: arms},
		},
	})
	if err != nil {
		return fmt.Errorf("put item: %w", err)
	}
	return nil
}

func parseN(v types.AttributeValue) (int64, error) {
	n, ok := v.(*types.AttributeValueMemberN)
	if !ok || n == nil {
		return 0, nil
	}
	return strconv.ParseInt(n.Value, 10, 64)
}

func parseF(v types.AttributeValue) (float64, error) {
	n, ok := v.(*types.AttributeValueMemberN)
	if !ok || n == nil {
		return 0, nil
	}
	return strconv.ParseFloat(n.Value, 64)
}
