package opsprotocol

import "testing"

func TestValidateAndCompareReleaseVersions(t *testing.T) {
	t.Parallel()

	valid := []string{"v1.0.0", "v12.34.56", "v2.0.0-rc.1"}
	for _, version := range valid {
		if err := ValidateReleaseVersion(version); err != nil {
			t.Fatalf("expected %s to be valid: %v", version, err)
		}
	}

	invalid := []string{"latest", "1.0.0", "v1.0", "v1.01.0", "v1.0.0-", "v1.0.0/evil"}
	for _, version := range invalid {
		if err := ValidateReleaseVersion(version); err == nil {
			t.Fatalf("expected %s to be invalid", version)
		}
	}

	comparison, err := CompareReleaseVersions("v1.0.10", "v1.0.11")
	if err != nil {
		t.Fatal(err)
	}
	if comparison != -1 {
		t.Fatalf("expected older version comparison, got %d", comparison)
	}

	comparison, err = CompareReleaseVersions("v1.0.11", "v1.0.11-rc.1")
	if err != nil {
		t.Fatal(err)
	}
	if comparison != 1 {
		t.Fatalf("expected stable release to sort after prerelease, got %d", comparison)
	}
}
