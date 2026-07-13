package session

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const conversationCookiePrefix = "conv_key_"

type cookieDeleteSpec struct {
	Name         string
	Domain       string
	Path         string
	PartitionKey *network.CookiePartitionKey
}

func (s *Session) cleanupConversationCookies() error {
	if s.ctx == nil {
		return nil
	}

	var removed int
	err := chromedp.Run(s.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := network.Enable().Do(ctx); err != nil {
			return err
		}

		cookies, err := network.GetCookies().Do(ctx)
		if err != nil {
			return err
		}

		specs := convKeyDeleteSpecs(cookies)
		for _, spec := range specs {
			params := network.DeleteCookies(spec.Name).
				WithDomain(spec.Domain).
				WithPath(spec.Path)
			if spec.PartitionKey != nil {
				params = params.WithPartitionKey(spec.PartitionKey)
			}
			if err := params.Do(ctx); err != nil {
				return err
			}
		}

		removed = len(specs)
		return nil
	}))
	if err != nil {
		return err
	}

	if removed > 0 {
		fmt.Fprintf(os.Stderr, "[debug] cleanupConversationCookies: removed %d conv_key cookies\n", removed)
	}
	return nil
}

func convKeyDeleteSpecs(cookies []*network.Cookie) []cookieDeleteSpec {
	seen := make(map[string]struct{})
	specs := make([]cookieDeleteSpec, 0, len(cookies))

	for _, cookie := range cookies {
		if cookie == nil || !strings.HasPrefix(cookie.Name, conversationCookiePrefix) {
			continue
		}

		key := cookie.Name + "\x00" + cookie.Domain + "\x00" + cookie.Path + "\x00" + partitionKeyString(cookie.PartitionKey)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		specs = append(specs, cookieDeleteSpec{
			Name:         cookie.Name,
			Domain:       cookie.Domain,
			Path:         cookie.Path,
			PartitionKey: cookie.PartitionKey,
		})
	}

	return specs
}

func partitionKeyString(key *network.CookiePartitionKey) string {
	if key == nil {
		return ""
	}
	return key.TopLevelSite + "\x00" + fmt.Sprintf("%t", key.HasCrossSiteAncestor)
}
