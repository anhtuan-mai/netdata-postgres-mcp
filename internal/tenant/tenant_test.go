// SPDX-License-Identifier: GPL-3.0-or-later

package tenant

import (
	"testing"
)

func TestHashToken(t *testing.T) {
	hash := HashToken("my-secret-token")
	if hash == "" {
		t.Error("HashToken returned empty string")
	}
	if len(hash) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("hash length = %d, want 64", len(hash))
	}

	// Same input should produce same hash
	hash2 := HashToken("my-secret-token")
	if hash != hash2 {
		t.Error("HashToken not deterministic")
	}

	// Different input should produce different hash
	hash3 := HashToken("different-token")
	if hash == hash3 {
		t.Error("different inputs produced same hash")
	}
}

func TestFromContext_Nil(t *testing.T) {
	// FromContext on a context without tenant info should return nil.
	info := FromContext(t.Context())
	if info != nil {
		t.Errorf("expected nil, got %v", info)
	}
}
