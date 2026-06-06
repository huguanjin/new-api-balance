package handlers

import (
	"testing"

	"balanceserver/models"
)

func TestChannelsToSitesPrefersEnabledDuplicateURL(t *testing.T) {
	result := channelsToSites([]upstreamChannel{
		{ID: 1, Status: 1, Name: "enabled first", BaseURL: "https://example.com"},
		{ID: 2, Status: 2, Name: "disabled last", BaseURL: "https://example.com/"},
	}, existingSiteIndex{})

	if len(result.Sites) != 1 {
		t.Fatalf("expected 1 site after URL dedupe, got %d", len(result.Sites))
	}
	if result.Sites[0].ChannelID != 1 {
		t.Fatalf("expected enabled duplicate to be kept, got channel %d", result.Sites[0].ChannelID)
	}
	if result.DuplicateURLCount != 1 {
		t.Fatalf("expected 1 duplicate URL, got %d", result.DuplicateURLCount)
	}
}

func TestChannelsToSitesKeepsLastDuplicateWithSameStatus(t *testing.T) {
	result := channelsToSites([]upstreamChannel{
		{ID: 1, Status: 1, Name: "enabled first", BaseURL: "https://example.com"},
		{ID: 2, Status: 1, Name: "enabled last", BaseURL: "https://example.com/"},
	}, existingSiteIndex{})

	if len(result.Sites) != 1 {
		t.Fatalf("expected 1 site after URL dedupe, got %d", len(result.Sites))
	}
	if result.Sites[0].ChannelID != 2 {
		t.Fatalf("expected last same-status duplicate to be kept, got channel %d", result.Sites[0].ChannelID)
	}
}

func TestChannelsToSitesPreservesSettingsByCanonicalURL(t *testing.T) {
	key := siteURLDedupKey("https://example.com/api/user/self")
	result := channelsToSites([]upstreamChannel{
		{ID: 1, Status: 1, Name: "imported", BaseURL: "https://example.com"},
	}, existingSiteIndex{
		ByURL: map[string]models.Site{
			key: {
				URL:     "https://example.com/api/user/self",
				Token:   "token",
				UserID:  "user",
				Adapter: "ephone",
			},
		},
	})

	if len(result.Sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(result.Sites))
	}
	if result.Sites[0].Token != "token" || result.Sites[0].UserID != "user" || result.Sites[0].Adapter != "ephone" {
		t.Fatalf("expected settings to be preserved by canonical URL, got token=%q user=%q adapter=%q", result.Sites[0].Token, result.Sites[0].UserID, result.Sites[0].Adapter)
	}
}

func TestSiteURLDedupKeyStripsBalanceEndpoint(t *testing.T) {
	rootKey := siteURLDedupKey("https://example.com")
	endpointKey := siteURLDedupKey("https://example.com/api/user/self")
	if rootKey != endpointKey {
		t.Fatalf("expected root and balance endpoint to share dedupe key, got %q and %q", rootKey, endpointKey)
	}
}
