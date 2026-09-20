package google

import (
	"fmt"
	"net/url"
)

// Business Profile is three separate APIs on three separate hosts, unlike
// Gmail/Drive/Calendar's one — a location's reviews live under the account
// that owns it, so listing reviews at all means walking account -> location
// -> reviews first, per Google's own Account Management, Business
// Information and (legacy v4) reviews APIs.
//
// This has not been tried against a live account — this build has none —
// so treat the account/location resolution below as documentation-grounded
// (fetched from developers.google.com/my-business's own reference pages),
// not field-verified the way Calendar's rawEvent shape is. docs/connections.md
// says so plainly rather than claiming otherwise.
var (
	accountMgmtBase  = "https://mybusinessaccountmanagement.googleapis.com/v1"
	businessInfoBase = "https://mybusinessbusinessinformation.googleapis.com/v1"
	reviewsBaseV4    = "https://mybusiness.googleapis.com/v4"
)

// Review is the shape bots/review-responder/fixtures/gbp.reviews.list.json
// also uses — live and demo data share one shape.
type Review struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	Rating int    `json:"rating"`
	Text   string `json:"text"`
}

type rawAccount struct {
	Name string `json:"name"` // "accounts/{accountId}"
}

type rawLocation struct {
	Name string `json:"name"` // "locations/{locationId}"
}

type rawReview struct {
	ReviewID string `json:"reviewId"`
	Reviewer struct {
		DisplayName string `json:"displayName"`
		IsAnonymous bool   `json:"isAnonymous"`
	} `json:"reviewer"`
	StarRating string `json:"starRating"` // STAR_RATING_UNSPECIFIED | ONE .. FIVE
	Comment    string `json:"comment"`
	CreateTime string `json:"createTime"` // RFC3339, UTC
}

var starRatingValue = map[string]int{
	"ONE": 1, "TWO": 2, "THREE": 3, "FOUR": 4, "FIVE": 5,
}

func (r rawReview) author() string {
	if r.Reviewer.IsAnonymous || r.Reviewer.DisplayName == "" {
		return "Anonymous"
	}
	return r.Reviewer.DisplayName
}

// ReviewsList implements `reviews.list`: every review at the first location
// of the first account this connection can see, with createTime on or
// after since (RFC3339).
//
// "First account, first location" is a real, disclosed simplification, the
// same shape as EventsList defaulting to the "primary" calendar — a
// business with more than one Business Profile location only gets the
// first one's reviews until this grows a location selector. since is
// applied client-side, filtering every review in the first page rather
// than stopping early: v4's reviews.list has no created-after filter, only
// an orderBy (updateTime desc by default), and an edited review's
// updateTime can be newer than an unedited one's createTime — so createTime
// order is not guaranteed and a first-match-and-stop scan could skip a
// genuinely new review sitting behind an old one that was recently edited.
// This also means only the first page is ever seen; pagination is a real
// gap for a location with more reviews than fit on one.
func (c *Client) ReviewsList(since string) ([]Review, error) {
	accounts, err := c.accountsList()
	if err != nil {
		return nil, fmt.Errorf("reviews.list: %w", err)
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("reviews.list: no Business Profile accounts on this connection")
	}
	locations, err := c.locationsList(accounts[0].Name)
	if err != nil {
		return nil, fmt.Errorf("reviews.list: %w", err)
	}
	if len(locations) == 0 {
		return nil, fmt.Errorf("reviews.list: account %s has no Business Profile locations", accounts[0].Name)
	}
	raw, err := c.reviewsListRaw(locations[0].Name)
	if err != nil {
		return nil, fmt.Errorf("reviews.list: %w", err)
	}
	out := make([]Review, 0, len(raw))
	for _, r := range raw {
		if since != "" && r.CreateTime < since {
			continue
		}
		out = append(out, Review{
			ID:     r.ReviewID,
			Author: r.author(),
			Rating: starRatingValue[r.StarRating],
			Text:   r.Comment,
		})
	}
	return out, nil
}

func (c *Client) accountsList() ([]rawAccount, error) {
	var resp struct {
		Accounts []rawAccount `json:"accounts"`
	}
	if err := c.getJSON(accountMgmtBase+"/accounts", &resp); err != nil {
		return nil, err
	}
	return resp.Accounts, nil
}

func (c *Client) locationsList(accountName string) ([]rawLocation, error) {
	listURL := fmt.Sprintf("%s/%s/locations?readMask=%s",
		businessInfoBase, accountName, url.QueryEscape("name,title"))
	var resp struct {
		Locations []rawLocation `json:"locations"`
	}
	if err := c.getJSON(listURL, &resp); err != nil {
		return nil, err
	}
	return resp.Locations, nil
}

func (c *Client) reviewsListRaw(locationName string) ([]rawReview, error) {
	var resp struct {
		Reviews []rawReview `json:"reviews"`
	}
	if err := c.getJSON(fmt.Sprintf("%s/%s/reviews", reviewsBaseV4, locationName), &resp); err != nil {
		return nil, err
	}
	return resp.Reviews, nil
}
