package session

import (
	"testing"

	"github.com/chromedp/cdproto/network"
)

func TestConvKeyDeleteSpecsFiltersAndDeduplicates(t *testing.T) {
	partitionKey := &network.CookiePartitionKey{
		TopLevelSite:         "https://chatgpt.com",
		HasCrossSiteAncestor: false,
	}

	cookies := []*network.Cookie{
		{Name: "conv_key_1", Domain: "chatgpt.com", Path: "/"},
		{Name: "conv_key_1", Domain: "chatgpt.com", Path: "/"},
		{Name: "conv_key_2", Domain: ".chatgpt.com", Path: "/", PartitionKey: partitionKey},
		{Name: "other_cookie", Domain: "chatgpt.com", Path: "/"},
		nil,
	}

	got := convKeyDeleteSpecs(cookies)
	if len(got) != 2 {
		t.Fatalf("got %d delete specs, want 2", len(got))
	}

	if got[0].Name != "conv_key_1" || got[0].Domain != "chatgpt.com" || got[0].Path != "/" {
		t.Fatalf("unexpected first spec: %+v", got[0])
	}

	if got[1].Name != "conv_key_2" || got[1].Domain != ".chatgpt.com" || got[1].Path != "/" {
		t.Fatalf("unexpected second spec: %+v", got[1])
	}

	if got[1].PartitionKey == nil || got[1].PartitionKey.TopLevelSite != "https://chatgpt.com" {
		t.Fatalf("expected partition key to be preserved, got %+v", got[1].PartitionKey)
	}
}
