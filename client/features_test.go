package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// featuresPayload is shaped like the real response, envelope included, and
// carries the id and timestamps that Feature does not model. Decoding has to
// survive fields we ignore, since that is what a BloodHound upgrade adds.
const featuresPayload = `{"data":[
	{"id":1,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z",
	 "key":"opengraph_extension_management","name":"OpenGraph Extension Management",
	 "description":"Enables the extensions endpoint","enabled":true,"user_updatable":true},
	{"id":2,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z",
	 "key":"use_raw_object_id","name":"Use Raw Object ID",
	 "description":"Preserves the original case of object IDs","enabled":false,"user_updatable":false}
]}`

func TestFeaturesAreKeyedByFlagKey(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		verifyLikeBloodHound(t, r)
		if r.URL.Path != pathFeatures {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		fmt.Fprint(w, featuresPayload)
	})

	flags, err := c.Features(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 2 {
		t.Fatalf("got %d flags, want 2", len(flags))
	}
	raw, ok := flags[FeatureFlagRawObjectIDs]
	if !ok {
		t.Fatalf("missing %q among the %d flags decoded", FeatureFlagRawObjectIDs, len(flags))
	}
	if raw.Enabled {
		t.Error("use_raw_object_id should be off in this payload")
	}
	if raw.UserUpdatable {
		t.Error("use_raw_object_id should not be user updatable in this payload")
	}
}

func TestFeatureEnabledReadsTheNamedFlag(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, featuresPayload)
	})

	for _, tc := range []struct {
		key  string
		want bool
	}{
		{FeatureFlagExtensions, true},
		{FeatureFlagRawObjectIDs, false},
	} {
		got, err := c.FeatureEnabled(context.Background(), tc.key)
		if err != nil {
			t.Fatalf("%s: %v", tc.key, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.key, got, tc.want)
		}
	}
}

// A flag the instance never reported must not come back as a plain false, which
// would be indistinguishable from one that is switched off.
func TestFeatureEnabledRejectsAnUnknownFlag(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, featuresPayload)
	})

	_, err := c.FeatureEnabled(context.Background(), "no_such_flag")
	if err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
	if !strings.Contains(err.Error(), "no_such_flag") {
		t.Errorf("the error should name the flag, got: %v", err)
	}
}

// The endpoint is permission gated, so a narrow token gets a 403 that says
// nothing about which permission is missing.
func TestFeaturesForbiddenNamesThePermission(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":[{"message":"forbidden"}]}`, http.StatusForbidden)
	})

	_, err := c.Features(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "application configuration") {
		t.Errorf("the 403 should point at the permission, got: %v", err)
	}
}
