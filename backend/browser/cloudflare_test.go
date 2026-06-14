package browser

import "testing"

func TestIsCloudflareChallengeText(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"Just a moment...\nChecking your browser before accessing", true},
		{"Security by Cloudflare", true},
		{"Please wait while we check malicious bots", true},
		{"Creeper | Minecraft Wiki | Fandom", false},
		{"Example Domain\nThis domain is for use in illustrative examples", false},
	}
	for _, tc := range cases {
		if got := IsCloudflareChallengeText(tc.text); got != tc.want {
			t.Errorf("IsCloudflareChallengeText(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
