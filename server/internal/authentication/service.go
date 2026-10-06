package authentication

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Rahmannugar/authlier"
	"github.com/Rahmannugar/authlier/emailpassword"
	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/google/uuid"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrSuspended       = errors.New("account suspended")
	ErrInvalidUsername = errors.New("username must be 1 to 64 characters")
)

type SessionResolver interface {
	ResolveSession(*http.Request) (authlier.Session, error)
}

type Users interface {
	ResolveUser(ctx context.Context, subjectID string, role string) (models.User, error)
	UpdateUsername(ctx context.Context, id uuid.UUID, username *string) (models.User, error)
}

type Session struct {
	ID        string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type Account struct {
	Session Session
	User    models.User
	Email   string
}

type Service struct {
	sessions    SessionResolver
	users       Users
	credentials emailpassword.CredentialStore
}

func NewService(
	sessions SessionResolver,
	users Users,
	credentials emailpassword.CredentialStore,
) *Service {
	return &Service{sessions: sessions, users: users, credentials: credentials}
}

func (service *Service) Resolve(request *http.Request) (Account, error) {
	session, err := service.sessions.ResolveSession(request)
	if err != nil {
		return Account{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	user, err := service.users.ResolveUser(request.Context(), session.SubjectID, models.RoleUser)
	if err != nil {
		return Account{}, fmt.Errorf("resolve user: %w", err)
	}
	if user.Status == models.StatusSuspended {
		return Account{}, ErrSuspended
	}
	identity, _, err := service.credentials.FindBySubject(request.Context(), session.SubjectID)
	if err != nil {
		return Account{}, fmt.Errorf("resolve identity: %w", err)
	}
	return Account{
		Session: Session{
			ID:        session.ID,
			CreatedAt: session.CreatedAt,
			ExpiresAt: session.ExpiresAt,
		},
		User:  user,
		Email: identity.Email,
	}, nil
}

func (service *Service) SetUsername(
	ctx context.Context,
	account Account,
	rawUsername string,
) (Account, error) {
	username := strings.TrimSpace(rawUsername)
	if username == "" || len(username) > 64 {
		return Account{}, ErrInvalidUsername
	}
	user, err := service.users.UpdateUsername(ctx, account.User.ID, &username)
	if err != nil {
		return Account{}, fmt.Errorf("set username: %w", err)
	}
	account.User = user
	return account, nil
}
