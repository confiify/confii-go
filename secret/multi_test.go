// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	name        string
	getValue    any
	getErr      error
	listValues  []string
	listErr     error
	setErr      error
	deleteErr   error
	getCalls    int
	listCalls   int
	setCalls    int
	deleteCalls int
}

func (f *fakeStore) Name() string { return f.name }

func (f *fakeStore) GetSecret(_ context.Context, _ string, _ ...confii.SecretOption) (any, error) {
	f.getCalls++
	return f.getValue, f.getErr
}

func (f *fakeStore) SetSecret(_ context.Context, _ string, _ any, _ ...confii.SecretOption) error {
	f.setCalls++
	return f.setErr
}

func (f *fakeStore) DeleteSecret(_ context.Context, _ string, _ ...confii.SecretOption) error {
	f.deleteCalls++
	return f.deleteErr
}

func (f *fakeStore) ListSecrets(_ context.Context, _ string) ([]string, error) {
	f.listCalls++
	return f.listValues, f.listErr
}

func TestMultiStore_Fallback(t *testing.T) {
	primary := NewDictStore(map[string]any{"key1": "from-primary"})
	secondary := NewDictStore(map[string]any{"key2": "from-secondary"})

	multi := NewMultiStore([]confii.SecretStore{primary, secondary})
	ctx := context.Background()

	val, err := multi.GetSecret(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "from-primary", val)

	val, err = multi.GetSecret(ctx, "key2")
	require.NoError(t, err)
	assert.Equal(t, "from-secondary", val)
}

func TestMultiStore_NotFound(t *testing.T) {
	multi := NewMultiStore([]confii.SecretStore{
		NewDictStore(nil),
	})

	_, err := multi.GetSecret(context.Background(), "missing")
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound))
}

func TestMultiStore_WriteToFirst(t *testing.T) {
	primary := NewDictStore(nil)
	secondary := NewDictStore(nil)

	multi := NewMultiStore([]confii.SecretStore{primary, secondary}, WithWriteToFirst(true))
	ctx := context.Background()

	require.NoError(t, multi.SetSecret(ctx, "key", "value"))

	val, _ := primary.GetSecret(ctx, "key")
	assert.Equal(t, "value", val)

	_, err := secondary.GetSecret(ctx, "key")
	assert.Error(t, err)
}

func TestMultiStore_ListSecrets(t *testing.T) {
	s1 := NewDictStore(map[string]any{"a": 1, "b": 2})
	s2 := NewDictStore(map[string]any{"b": 3, "c": 4})

	multi := NewMultiStore([]confii.SecretStore{s1, s2})
	keys, err := multi.ListSecrets(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, keys, 3)
}

func TestMultiStore_GetSecret_AllStoresNotFound_ReturnsNotFound(t *testing.T) {
	notFound1 := fmt.Errorf("%w: %s", confii.ErrSecretNotFound, "k")
	notFound2 := fmt.Errorf("%w: %s", confii.ErrSecretNotFound, "k")
	s1 := &fakeStore{name: "s1", getErr: notFound1}
	s2 := &fakeStore{name: "s2", getErr: notFound2}

	multi := NewMultiStore([]confii.SecretStore{s1, s2})
	val, err := multi.GetSecret(context.Background(), "k")

	assert.Nil(t, val)
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound),
		"all-not-found should still satisfy errors.Is(ErrSecretNotFound)")

	assert.Equal(t, 1, s1.getCalls)
	assert.Equal(t, 1, s2.getCalls)
}

func TestMultiStore_GetSecret_RealErrorWithoutSuccess_ReturnsJoinedError(t *testing.T) {
	awsErr := errors.New("aws unavailable")
	notFound := fmt.Errorf("%w: %s", confii.ErrSecretNotFound, "k")
	s1 := &fakeStore{name: "aws", getErr: awsErr}
	s2 := &fakeStore{name: "dict", getErr: notFound}

	multi := NewMultiStore([]confii.SecretStore{s1, s2})
	val, err := multi.GetSecret(context.Background(), "k")

	assert.Nil(t, val)
	require.Error(t, err)

	assert.False(t, errors.Is(err, confii.ErrSecretNotFound),
		"real backend errors must not be hidden behind ErrSecretNotFound")

	assert.True(t, errors.Is(err, awsErr), "joined error should expose the AWS error")

	msg := err.Error()
	assert.Contains(t, msg, "aws unavailable")
	assert.Contains(t, msg, "store[0]")

	var mse *MultiStoreError
	require.True(t, errors.As(err, &mse))
	assert.Equal(t, "GetSecret", mse.Op)
	assert.Equal(t, "k", mse.Key)
	require.Len(t, mse.Errs, 1)
	assert.Equal(t, "aws", mse.Errs[0].Name)
	assert.Equal(t, 0, mse.Errs[0].Index)
}

func TestMultiStore_GetSecret_BackendErrorStopsFallback(t *testing.T) {
	s1 := &fakeStore{name: "broken", getErr: errors.New("network down")}
	s2 := &fakeStore{name: "ok", getValue: "the-secret"}

	multi := NewMultiStore([]confii.SecretStore{s1, s2})
	val, err := multi.GetSecret(context.Background(), "k")

	require.Error(t, err)
	assert.Nil(t, val)
	assert.Equal(t, 1, s1.getCalls)
	assert.Equal(t, 0, s2.getCalls)
}

func TestMultiStore_GetSecret_OrderMatters(t *testing.T) {
	s1 := &fakeStore{name: "first", getValue: "first-wins"}
	s2 := &fakeStore{name: "second", getValue: "should-not-be-asked"}

	multi := NewMultiStore([]confii.SecretStore{s1, s2})
	val, err := multi.GetSecret(context.Background(), "k")

	require.NoError(t, err)
	assert.Equal(t, "first-wins", val)
	assert.Equal(t, 1, s1.getCalls)
	assert.Equal(t, 0, s2.getCalls, "second store must not be queried after first succeeds")
}

func TestMultiStore_GetSecret_AllNotFoundReturnsTypedError(t *testing.T) {
	notFound := fmt.Errorf("%w:%s", confii.ErrSecretNotFound, "k")
	s1 := &fakeStore{name: "s1", getErr: notFound}

	multi := NewMultiStore([]confii.SecretStore{s1})
	val, err := multi.GetSecret(context.Background(), "k")

	require.ErrorIs(t, err, confii.ErrSecretNotFound)
	assert.Nil(t, val)
}

func TestMultiStore_GetSecret_FailOnMissingDisabled_RealErrorStillSurfaces(t *testing.T) {

	awsErr := errors.New("aws unavailable")
	s1 := &fakeStore{name: "aws", getErr: awsErr}

	multi := NewMultiStore([]confii.SecretStore{s1})
	val, err := multi.GetSecret(context.Background(), "k")

	assert.Nil(t, val)
	require.Error(t, err)
	assert.True(t, errors.Is(err, awsErr))
	assert.False(t, errors.Is(err, confii.ErrSecretNotFound))
}

func TestMultiStore_ListSecrets_PartialFailure_ReturnsBothInventoryAndError(t *testing.T) {
	s1 := &fakeStore{name: "ok", listValues: []string{"a", "b"}}
	s2 := &fakeStore{name: "broken", listErr: errors.New("vault sealed")}

	multi := NewMultiStore([]confii.SecretStore{s1, s2})
	keys, err := multi.ListSecrets(context.Background(), "")

	require.Error(t, err, "partial failure must surface an error")
	assert.ElementsMatch(t, []string{"a", "b"}, keys, "partial inventory must still be returned")

	var mse *MultiStoreError
	require.True(t, errors.As(err, &mse))
	assert.Equal(t, "ListSecrets", mse.Op)
	require.Len(t, mse.Errs, 1)
	assert.Equal(t, 1, mse.Errs[0].Index)
	assert.Equal(t, "broken", mse.Errs[0].Name)
}

func TestMultiStore_ListSecrets_AllStoresSucceed_NoError(t *testing.T) {
	s1 := &fakeStore{name: "s1", listValues: []string{"a", "b"}}
	s2 := &fakeStore{name: "s2", listValues: []string{"b", "c"}}

	multi := NewMultiStore([]confii.SecretStore{s1, s2})
	keys, err := multi.ListSecrets(context.Background(), "")

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "b", "c"}, keys)
}

func TestMultiStore_ListSecrets_AllStoresFail_NoInventory(t *testing.T) {
	e1 := errors.New("vault sealed")
	e2 := errors.New("aws timeout")
	s1 := &fakeStore{name: "vault", listErr: e1}
	s2 := &fakeStore{name: "aws", listErr: e2}

	multi := NewMultiStore([]confii.SecretStore{s1, s2})
	keys, err := multi.ListSecrets(context.Background(), "")

	assert.Empty(t, keys)
	require.Error(t, err)
	assert.True(t, errors.Is(err, e1))
	assert.True(t, errors.Is(err, e2))

	var mse *MultiStoreError
	require.True(t, errors.As(err, &mse))
	require.Len(t, mse.Errs, 2)

	msg := err.Error()
	assert.Contains(t, msg, "vault sealed")
	assert.Contains(t, msg, "aws timeout")
	assert.True(t, strings.Contains(msg, "store[0]"))
	assert.True(t, strings.Contains(msg, "store[1]"))
}

func TestMultiStoreError_ErrorAndUnwrap_NilSafe(t *testing.T) {
	var mse *MultiStoreError

	assert.NotPanics(t, func() { _ = mse.Error() })
	assert.Nil(t, mse.Unwrap())

	mse = &MultiStoreError{Op: "GetSecret", Key: "k"}
	assert.Contains(t, mse.Error(), "(no entries)")
	assert.Empty(t, mse.Unwrap())
}
