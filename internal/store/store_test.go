package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "admin.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStore_CreateAndAuthenticate(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	acc, err := s.CreateAccount(ctx, "admin", "secret123")
	require.NoError(t, err)
	assert.Equal(t, "admin", acc.Account)

	_, err = s.CreateAccount(ctx, "admin", "other")
	assert.ErrorIs(t, err, ErrAccountExists)

	got, err := s.Authenticate(ctx, "admin", "secret123")
	require.NoError(t, err)
	assert.Equal(t, acc.ID, got.ID)

	_, err = s.Authenticate(ctx, "admin", "wrong")
	assert.ErrorIs(t, err, ErrInvalidCredential)

	_, err = s.Authenticate(ctx, "nobody", "secret123")
	assert.ErrorIs(t, err, ErrInvalidCredential)
}

func TestStore_SessionLifecycle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	acc, err := s.CreateAccount(ctx, "admin", "secret123")
	require.NoError(t, err)

	token, err := s.CreateSession(ctx, acc.ID, time.Hour)
	require.NoError(t, err)
	assert.Len(t, token, 64)

	got, err := s.GetSession(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, "admin", got.Account)

	require.NoError(t, s.DeleteSession(ctx, token))
	_, err = s.GetSession(ctx, token)
	assert.ErrorIs(t, err, ErrSessionNotFound)

	expired, err := s.CreateSession(ctx, acc.ID, -time.Minute)
	require.NoError(t, err)
	_, err = s.GetSession(ctx, expired)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestStore_DeleteAccountKeepsLastOne(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a, err := s.CreateAccount(ctx, "a", "secret123")
	require.NoError(t, err)

	assert.ErrorIs(t, s.DeleteAccount(ctx, a.ID), ErrLastAccount)

	b, err := s.CreateAccount(ctx, "b", "secret123")
	require.NoError(t, err)
	assert.ErrorIs(t, s.DeleteAccount(ctx, 999), ErrAccountNotFound)
	require.NoError(t, s.DeleteAccount(ctx, b.ID))
	assert.ErrorIs(t, s.DeleteAccount(ctx, a.ID), ErrLastAccount)
}

func TestStore_UpdatePassword(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a, err := s.CreateAccount(ctx, "a", "oldpass123")
	require.NoError(t, err)

	assert.ErrorIs(t, s.UpdatePassword(ctx, a.ID, "wrong", "newpass123"), ErrInvalidCredential)
	require.NoError(t, s.UpdatePassword(ctx, a.ID, "oldpass123", "newpass123"))

	_, err = s.Authenticate(ctx, "a", "newpass123")
	assert.NoError(t, err)
}
