package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackgroundSessionBelongsTo(t *testing.T) {
	const id = "867f943e-e370-46ae-af21-9e973c0e1c05"
	for _, tc := range []struct {
		name    string
		session backgroundSession
		want    bool
	}{
		{"background entry, as claude agents --json lists it", backgroundSession{ID: "867f943e", SessionID: id, Kind: "background", Name: "other"}, true},
		{"short id only", backgroundSession{ID: "867f943e", Kind: "background"}, true},
		{"same name, other conversation", backgroundSession{ID: "aaaa1111", Kind: "background", Name: "openstreetmap-fix", Cwd: "/foys-all"}, false},
		{"interactive session", backgroundSession{SessionID: id, Kind: "interactive"}, false},
	} {
		require.Equal(t, tc.want, tc.session.belongsTo(id, "openstreetmap-fix", "/foys-all"), tc.name)
	}

	// Without the conversation's id, the name cs gave the session decides.
	named := backgroundSession{ID: "aaaa1111", Kind: "background", Name: "openstreetmap-fix", Cwd: "/foys-all"}
	require.True(t, named.belongsTo("", "openstreetmap-fix", "/foys-all"))
	require.False(t, named.belongsTo("", "openstreetmap-fix", "/elsewhere"))
}
