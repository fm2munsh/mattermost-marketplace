package store

import (
	"bytes"
	"testing"

	"github.com/mattermost/mattermost-marketplace/internal/testlib"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Run("empty stream", func(t *testing.T) {
		logger := testlib.MakeLogger(t)
		store, err := New(bytes.NewReader([]byte{}), logger)
		require.NoError(t, err)
		require.NotNil(t, store)
		require.Empty(t, store.plugins)
	})

	t.Run("invalid stream", func(t *testing.T) {
		logger := testlib.MakeLogger(t)
		store, err := New(bytes.NewReader([]byte(`{"invalid":`)), logger)
		require.EqualError(t, err, "failed to parse stream: unexpected EOF")
		require.Nil(t, store)
	})

	t.Run("missing manifest id", func(t *testing.T) {
		logger := testlib.MakeLogger(t)
		store, err := New(bytes.NewReader([]byte(`[{"HomepageURL":"https://github.com/mattermost/mattermost-plugin-demo","IconData":"icon-data.svg","DownloadURL":"https://github.com/mattermost/mattermost-plugin-demo/releases/download/v0.1.0/com.mattermost.demo-plugin-0.1.0.tar.gz","DownloadSignature":"c2lnbmF0dXJl","ReleaseNotesURL":"https://github.com/mattermost/mattermost-plugin-demo/releases/v0.1.0","Manifest":{}},{"HomepageURL":"https://github.com/mattermost/mattermost-plugin-starter-template","DownloadURL":"https://github.com/mattermost/mattermost-plugin-starter-template/releases/download/v0.1.0/com.mattermost.plugin-starter-template-0.1.0.tar.gz","Signatures":[{"signature":"signature2","public_key_hash":"hash2"}],"ReleaseNotesURL":"https://github.com/mattermost/mattermost-plugin-starter-template/releases/v0.1.0","Manifest":{}}]`)), logger)
		require.Contains(t, err.Error(), "failed to validate plugins: plugin manifest Id is empty ")
		require.Nil(t, store)
	})

	t.Run("missing manifest version", func(t *testing.T) {
		logger := testlib.MakeLogger(t)
		store, err := New(bytes.NewReader([]byte(`[{"HomepageURL":"https://github.com/mattermost/mattermost-plugin-demo","IconData":"icon-data.svg","DownloadURL":"https://github.com/mattermost/mattermost-plugin-demo/releases/download/v0.1.0/com.mattermost.demo-plugin-0.1.0.tar.gz","DownloadSignature":"c2lnbmF0dXJl","ReleaseNotesURL":"https://github.com/mattermost/mattermost-plugin-demo/releases/v0.1.0","Manifest":{"id": "test"}},{"HomepageURL":"https://github.com/mattermost/mattermost-plugin-starter-template","DownloadURL":"https://github.com/mattermost/mattermost-plugin-starter-template/releases/download/v0.1.0/com.mattermost.plugin-starter-template-0.1.0.tar.gz","Signatures":[{"signature":"signature2","public_key_hash":"hash2"}],"ReleaseNotesURL":"https://github.com/mattermost/mattermost-plugin-starter-template/releases/v0.1.0","Manifest":{"id": "test"}}]`)), logger)
		require.EqualError(t, err, "failed to validate plugins: failed to parse manifest version for manifest.Id test: Version string empty")
		require.Nil(t, store)
	})

	t.Run("missing min_server_version version is valid", func(t *testing.T) {
		logger := testlib.MakeLogger(t)
		store, err := New(bytes.NewReader([]byte(`[{"HomepageURL":"https://github.com/mattermost/mattermost-plugin-demo","IconData":"icon-data.svg","DownloadURL":"https://github.com/mattermost/mattermost-plugin-demo/releases/download/v0.1.0/com.mattermost.demo-plugin-0.1.0.tar.gz","DownloadSignature":"c2lnbmF0dXJl","ReleaseNotesURL":"https://github.com/mattermost/mattermost-plugin-demo/releases/v0.1.0","Manifest":{"id": "test", "version": "0.1.0"}},{"HomepageURL":"https://github.com/mattermost/mattermost-plugin-starter-template","DownloadURL":"https://github.com/mattermost/mattermost-plugin-starter-template/releases/download/v0.1.0/com.mattermost.plugin-starter-template-0.1.0.tar.gz","Signatures":[{"signature":"signature2","public_key_hash":"hash2"}],"ReleaseNotesURL":"https://github.com/mattermost/mattermost-plugin-starter-template/releases/v0.1.0","Manifest":{"id": "test", "version": "0.1.0"}}]`)), logger)
		require.NoError(t, err)
		require.NotNil(t, store)
	})

	t.Run("valid stream", func(t *testing.T) {
		logger := testlib.MakeLogger(t)
		store, err := New(bytes.NewReader([]byte(`[{"HomepageURL":"https://github.com/mattermost/mattermost-plugin-demo","IconData":"icon-data.svg","DownloadURL":"https://github.com/mattermost/mattermost-plugin-demo/releases/download/v0.1.0/com.mattermost.demo-plugin-0.1.0.tar.gz","DownloadSignature":"c2lnbmF0dXJl","ReleaseNotesURL":"https://github.com/mattermost/mattermost-plugin-demo/releases/v0.1.0","Manifest":{"id": "test", "version": "0.1.0"}},{"HomepageURL":"https://github.com/mattermost/mattermost-plugin-starter-template","DownloadURL":"https://github.com/mattermost/mattermost-plugin-starter-template/releases/download/v0.1.0/com.mattermost.plugin-starter-template-0.1.0.tar.gz","Signatures":[{"signature":"signature2","public_key_hash":"hash2"}],"ReleaseNotesURL":"https://github.com/mattermost/mattermost-plugin-starter-template/releases/v0.1.0","Manifest":{"id": "test", "version": "0.1.0"}}]`)), logger)
		require.NoError(t, err)
		require.NotNil(t, store)
	})
}
