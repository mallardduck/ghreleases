package ghrelease

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"1.0.0", "v1.0.0", 0},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.10.0", "v1.9.0", 1},
		{"v2.0.0", "v1.99.99", 1},
		// main version outranks prerelease identifiers
		{"v1.1.0-alpha", "v1.0.0", 1},
		// a prerelease sorts below its release
		{"v1.0.0-rc.1", "v1.0.0", -1},
		// semver spec section 11 precedence chain
		{"v1.0.0-alpha", "v1.0.0-alpha.1", -1},
		{"v1.0.0-alpha.1", "v1.0.0-alpha.beta", -1},
		{"v1.0.0-alpha.beta", "v1.0.0-beta", -1},
		{"v1.0.0-beta", "v1.0.0-beta.2", -1},
		{"v1.0.0-beta.2", "v1.0.0-beta.11", -1},
		{"v1.0.0-beta.11", "v1.0.0-rc.1", -1},
		// numeric identifiers beyond uint64 still order numerically
		{"v1.0.0-alpha.99999999999999999999", "v1.0.0-alpha.100000000000000000000", -1},
		{"v1.0.0-99999999999999999999", "v1.0.0-alpha", -1},
		// build metadata is ignored
		{"v1.0.0+a", "v1.0.0+b", 0},
		// prefixed tags compare on the version part
		{"cli/v1.0.0", "cli/v1.1.0", -1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got, ok := CompareVersions(tt.a, tt.b)
			if !ok {
				t.Fatalf("CompareVersions(%q, %q) ok = false", tt.a, tt.b)
			}
			if got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			if rev, _ := CompareVersions(tt.b, tt.a); rev != -tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d (antisymmetry)", tt.b, tt.a, rev, -tt.want)
			}
		})
	}
}

func TestCompareVersions_Unparseable(t *testing.T) {
	for _, pair := range [][2]string{{"nightly", "v1.0.0"}, {"v1.0.0", "latest"}, {"v1.0", "v1.0.0"},
		// leading zeros are not valid semver (§2, §9)
		{"v1.02.3", "v1.0.0"}, {"v01.2.3", "v1.0.0"}, {"v1.2.03", "v1.0.0"}, {"v1.0.0-01", "v1.0.0"},
		// core numbers beyond uint64 do not parse
		{"v1.99999999999999999999.0", "v1.0.0"},
	} {
		if _, ok := CompareVersions(pair[0], pair[1]); ok {
			t.Errorf("CompareVersions(%q, %q) ok = true, want false", pair[0], pair[1])
		}
	}
}
