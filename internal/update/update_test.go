package update

import "testing"

func TestUpdateEndpointsPointToForkRepository(t *testing.T) {
	if updateRepo != "mofeiss/octopus" {
		t.Fatalf("expected update repo to use fork, got %q", updateRepo)
	}
	if updateApiUrl != "https://api.github.com/repos/mofeiss/octopus/releases/latest" {
		t.Fatalf("unexpected update api url: %s", updateApiUrl)
	}
	if updateUrl != "https://github.com/mofeiss/octopus/releases/latest/download" {
		t.Fatalf("unexpected update download url: %s", updateUrl)
	}
}
