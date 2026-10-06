package authentication

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Rahmannugar/authlier"
	"github.com/Rahmannugar/authlier/emailpassword"
	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/google/uuid"
)

type fakeSessions struct {
	session authlier.Session
	err     error
}

func (sessions *fakeSessions) ResolveSession(*http.Request) (authlier.Session, error) {
	return sessions.session, sessions.err
}

type fakeUsers struct {
	user        models.User
	resolveErr  error
	updateErr   error
	gotSubject  string
	gotRole     string
	gotUsername *string
}

func (users *fakeUsers) ResolveUser(_ context.Context, subjectID string, role string) (models.User, error) {
	users.gotSubject = subjectID
	users.gotRole = role
	return users.user, users.resolveErr
}

func (users *fakeUsers) UpdateUsername(_ context.Context, id uuid.UUID, username *string) (models.User, error) {
	if users.updateErr != nil {
		return models.User{}, users.updateErr
	}
	users.gotUsername = username
	users.user.Username = username
	return users.user, nil
}

type fakeCredentials struct {
	email string
	err   error
}

func (credentials *fakeCredentials) FindBySubject(
	_ context.Context,
	subjectID string,
) (emailpassword.User, emailpassword.PasswordCredential, error) {
	if credentials.err != nil {
		return emailpassword.User{}, emailpassword.PasswordCredential{}, credentials.err
	}
	return emailpassword.User{ID: subjectID, Email: credentials.email},
		emailpassword.PasswordCredential{}, nil
}

func (credentials *fakeCredentials) AddPassword(context.Context, string, string, time.Time) (emailpassword.User, error) {
	return emailpassword.User{}, nil
}

func (credentials *fakeCredentials) ReplacePasswordHash(context.Context, string, string, string, time.Time) error {
	return nil
}

func (credentials *fakeCredentials) RemovePassword(context.Context, string, string, time.Time) error {
	return nil
}

func TestResolveProjectsActiveAccount(t *testing.T) {
	users := &fakeUsers{user: models.User{
		ID:        uuid.New(),
		SubjectID: "subject-1",
		Role:      models.RoleUser,
		Status:    models.StatusActive,
	}}
	service := NewService(
		&fakeSessions{session: authlier.Session{ID: "session-1", SubjectID: "subject-1"}},
		users,
		&fakeCredentials{email: "reader@example.com"},
	)

	account, err := service.Resolve(&http.Request{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if users.gotSubject != "subject-1" || users.gotRole != models.RoleUser {
		t.Errorf("projection subject/role = %q/%q, want subject-1/user", users.gotSubject, users.gotRole)
	}
	if account.Email != "reader@example.com" {
		t.Errorf("email = %q, want the identity email", account.Email)
	}
	if account.Session.ID != "session-1" {
		t.Errorf("session id = %q, want session-1", account.Session.ID)
	}
}

func TestResolveRejectsMissingSession(t *testing.T) {
	service := NewService(
		&fakeSessions{err: errors.New("no session")},
		&fakeUsers{},
		&fakeCredentials{},
	)

	if _, err := service.Resolve(&http.Request{}); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("error = %v, want unauthenticated", err)
	}
}

func TestResolveRejectsSuspendedAccount(t *testing.T) {
	service := NewService(
		&fakeSessions{session: authlier.Session{SubjectID: "subject-1"}},
		&fakeUsers{user: models.User{Status: models.StatusSuspended}},
		&fakeCredentials{email: "suspended@example.com"},
	)

	if _, err := service.Resolve(&http.Request{}); !errors.Is(err, ErrSuspended) {
		t.Errorf("error = %v, want suspended", err)
	}
}

func TestSetUsernameValidatesAndStores(t *testing.T) {
	users := &fakeUsers{user: models.User{ID: uuid.New()}}
	service := NewService(&fakeSessions{}, users, &fakeCredentials{})

	if _, err := service.SetUsername(context.Background(), Account{}, "   "); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("blank username error = %v, want invalid username", err)
	}
	if _, err := service.SetUsername(context.Background(), Account{}, strings.Repeat("a", 65)); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("long username error = %v, want invalid username", err)
	}

	account, err := service.SetUsername(context.Background(), Account{}, "  macrofan  ")
	if err != nil {
		t.Fatalf("SetUsername: %v", err)
	}
	if users.gotUsername == nil || *users.gotUsername != "macrofan" {
		t.Errorf("stored username = %v, want trimmed macrofan", users.gotUsername)
	}
	if account.User.Username == nil || *account.User.Username != "macrofan" {
		t.Errorf("account username = %v, want the stored value", account.User.Username)
	}
}
