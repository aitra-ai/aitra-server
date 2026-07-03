package metering

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeRows struct {
	rows        []tokenRow
	err         error
	gotCustomer string
}

func (f *fakeRows) ListTokenRows(_ context.Context, customerID string, _, _ time.Time) ([]tokenRow, error) {
	f.gotCustomer = customerID
	return f.rows, f.err
}

func TestOutputTokensParsesCompletionOnly(t *testing.T) {
	n, err := outputTokens(`{"prompt_token_num":"100","completion_token_num":"42","owner_type":"1"}`)
	require.NoError(t, err)
	require.Equal(t, uint64(42), n, "uses completion (output) tokens, not prompt/total")

	n, err = outputTokens(`{"prompt_token_num":"100"}`)
	require.NoError(t, err)
	require.Equal(t, uint64(0), n, "missing completion -> 0")

	n, err = outputTokens("")
	require.NoError(t, err)
	require.Equal(t, uint64(0), n)

	_, err = outputTokens("{not json")
	require.Error(t, err)
}

func TestWindowTokensSumsOutputTokens(t *testing.T) {
	fr := &fakeRows{rows: []tokenRow{
		{Extra: `{"completion_token_num":"42"}`},
		{Extra: `{"completion_token_num":"8"}`},
		{Extra: `bad json`},                  // skipped, not fatal
		{Extra: `{"prompt_token_num":"999"}`}, // no completion -> 0
	}}
	s := &Source{rows: fr}

	got, err := s.WindowTokens(context.Background(), "svc-a", time.UnixMilli(1000), time.UnixMilli(2000))
	require.NoError(t, err)
	require.Equal(t, uint64(50), got, "sum of completion tokens, malformed skipped, prompt ignored")
	require.Equal(t, "svc-a", fr.gotCustomer, "queries by metering customer key (SvcName)")
}

func TestWindowTokensEmptyKeyShortCircuits(t *testing.T) {
	fr := &fakeRows{rows: []tokenRow{{Extra: `{"completion_token_num":"42"}`}}}
	s := &Source{rows: fr}
	got, err := s.WindowTokens(context.Background(), "", time.UnixMilli(0), time.UnixMilli(1))
	require.NoError(t, err)
	require.Equal(t, uint64(0), got)
	require.Empty(t, fr.gotCustomer, "no key -> no query")
}

func TestNameIsProvenance(t *testing.T) {
	require.Equal(t, "account_metering", (&Source{}).Name())
}
