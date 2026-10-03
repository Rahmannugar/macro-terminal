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
	articleStore       = "articles"
	articleIDKey       = "article_id"
	publishedAtKey     = "published_at"
	storeRequestWindow = 60 * time.Second
)

var articlePredicates = []string{articleIDKey, publishedAtKey}

type grpcIndexer struct {
	conn   *grpc.ClientConn
	client aisvc.AIServiceClient
}

func newGrpcIndexer(addr string) (*grpcIndexer, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("create Ahnlich client for %q: %w", addr, err)
	}
	return &grpcIndexer{conn: conn, client: aisvc.NewAIServiceClient(conn)}, nil
}

func (indexer *grpcIndexer) StoreArticle(ctx context.Context, article Article) error {
	ctx, cancel := context.WithTimeout(ctx, storeRequestWindow)
	defer cancel()

	if err := indexer.ensureStore(ctx); err != nil {
		return err
	}

	condition := articleCondition(article.ID)
	existing, err := indexer.client.GetPred(ctx, &query.GetPred{
		Store:     articleStore,
		Condition: condition,
	})
	if err != nil {
		return fmt.Errorf("look up existing article vector: %w", err)
	}
	if len(existing.Entries) > 0 {
		if _, err := indexer.client.DelPred(ctx, &query.DelPred{
			Store:     articleStore,
			Condition: condition,
		}); err != nil {
			return fmt.Errorf("remove stale article vector: %w", err)
		}
	}

	_, err = indexer.client.Set(ctx, &query.Set{
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

func (indexer *grpcIndexer) ensureStore(ctx context.Context) error {
	_, err := indexer.client.CreateStore(ctx, &query.CreateStore{
		Store:      articleStore,
		QueryModel: models.AIModel_ALL_MINI_LM_L6_V2,
		IndexModel: models.AIModel_ALL_MINI_LM_L6_V2,
		Predicates: articlePredicates,
	})
	if err != nil {
		return fmt.Errorf("ensure article store: %w", err)
	}
	if _, err := indexer.client.CreatePredIndex(ctx, &query.CreatePredIndex{
		Store:      articleStore,
		Predicates: articlePredicates,
	}); err != nil {
		return fmt.Errorf("ensure article predicates: %w", err)
	}
	return nil
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
