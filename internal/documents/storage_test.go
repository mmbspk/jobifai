package documents_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestLocalBlobStore_PutAtomic_ConcurrentSameKey(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := documents.NewLocalBlobStore(root)
	require.NoError(t, err)
	key := "user-a/pdf/ab/cdeadbeef.pdf"
	payload := []byte(strings.Repeat("pdf-bytes", 400))

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sha, n, err := store.PutAtomic(key, bytes.NewReader(payload))
			require.NoError(t, err)
			require.Equal(t, int64(len(payload)), n)
			require.NotEmpty(t, sha)
		}()
	}
	wg.Wait()

	out, err := store.Read(key)
	require.NoError(t, err)
	require.Equal(t, payload, out)
}
