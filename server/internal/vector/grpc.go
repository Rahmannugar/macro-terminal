package vector

import (
	"context"
	"fmt"
	"time"

	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/ai/models"
	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/ai/preprocess"
	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/ai/query"
	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/keyval"
	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/metadata"
	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/predicates"
	aisvc "github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/services/ai_service"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	articleStore   = "articles"
	articleIDKey   = "article_id"
	publishedAtKey = "published_at"
	requestWindow  = 60 * time.Second
)

var articlePredicates = []string{articleIDKey, publishedAtKey}

type grpcClient struct {
	conn   *grpc.ClientConn
	client aisvc.AIServiceClient
}

func newGrpcClient(addr string) (*grpcClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("create Ahnlich client for %q: %w", addr, err)
	}
	return &grpcClient{conn: conn, client: aisvc.NewAIServiceClient(conn)}, nil
}

func (client *grpcClient) StoreArticle(ctx context.Context, article Article) error {
	ctx, cancel := context.WithTimeout(ctx, requestWindow)
	defer cancel()

	if err := client.ensureStore(ctx); err != nil {
		return err
	}

	condition := articleCondition(article.ID)
	existing, err := client.client.GetPred(ctx, &query.GetPred{
		Store:     articleStore,
		Condition: condition,
	})
	if err != nil {
		return fmt.Errorf("look up existing article vector: %w", err)
	}
	if len(existing.Entries) > 0 {
		if _, err := client.client.DelPred(ctx, &query.DelPred{
			Store:     articleStore,
			Condition: condition,
		}); err != nil {
			return fmt.Errorf("remove stale article vector: %w", err)
		}
	}

	_, err = client.client.Set(ctx, &query.Set{
		Store: articleStore,
		Inputs: []*keyval.AiStoreEntry{{
			Key: &keyval.StoreInput{
				Value: &keyval.StoreInput_RawString{RawString: embedInput(article)},
			},
			Value: &keyval.StoreValue{Value: map[string]*metadata.MetadataValue{
				articleIDKey:   metadataValue(article.ID.String()),
				publishedAtKey: metadataValue(formatPublishedAt(article.PublishedAt)),
			}},
		}},
		PreprocessAction: preprocess.PreprocessAction_NoPreprocessing,
	})
	if err != nil {
		return fmt.Errorf("store article vector: %w", err)
	}
	return nil
}

func (client *grpcClient) ensureStore(ctx context.Context) error {
	_, err := client.client.CreateStore(ctx, &query.CreateStore{
		Store:      articleStore,
		QueryModel: models.AIModel_ALL_MINI_LM_L6_V2,
		IndexModel: models.AIModel_ALL_MINI_LM_L6_V2,
		Predicates: articlePredicates,
	})
	if err != nil {
		return fmt.Errorf("ensure article store: %w", err)
	}
	if _, err := client.client.CreatePredIndex(ctx, &query.CreatePredIndex{
		Store:      articleStore,
		Predicates: articlePredicates,
	}); err != nil {
		return fmt.Errorf("ensure article predicates: %w", err)
	}
	return nil
}

func (client *grpcClient) SearchArticles(ctx context.Context, text string, limit int) ([]uuid.UUID, error) {
	if limit < 1 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, requestWindow)
	defer cancel()

	response, err := client.client.GetSimN(ctx, &query.GetSimN{
		Store: articleStore,
		SearchInput: &keyval.StoreInput{
			Value: &keyval.StoreInput_RawString{RawString: text},
		},
		ClosestN: uint64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search article vectors: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(response.Entries))
	for _, entry := range response.Entries {
		id, err := uuid.Parse(storeValueString(entry.Value, articleIDKey))
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func storeValueString(value *keyval.StoreValue, key string) string {
	if value == nil {
		return ""
	}
	item, ok := value.Value[key]
	if !ok || item == nil {
		return ""
	}
	raw, ok := item.Value.(*metadata.MetadataValue_RawString)
	if !ok {
		return ""
	}
	return raw.RawString
}

func articleCondition(articleID uuid.UUID) *predicates.PredicateCondition {
	return &predicates.PredicateCondition{
		Kind: &predicates.PredicateCondition_Value{
			Value: &predicates.Predicate{
				Kind: &predicates.Predicate_Equals{
					Equals: &predicates.Equals{
						Key:   articleIDKey,
						Value: metadataValue(articleID.String()),
					},
				},
			},
		},
	}
}

func embedInput(article Article) string {
	if article.Content == "" {
		return article.Title
	}
	return article.Title + "\n\n" + article.Content
}

func formatPublishedAt(publishedAt time.Time) string {
	if publishedAt.IsZero() {
		return ""
	}
	return publishedAt.UTC().Format(time.RFC3339)
}

func metadataValue(value string) *metadata.MetadataValue {
	return &metadata.MetadataValue{Value: &metadata.MetadataValue_RawString{RawString: value}}
}
