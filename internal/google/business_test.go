package google

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestReviewsListWalksAccountThenLocationThenReviews(t *testing.T) {
	var gotLocationsPath, gotReviewsPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"accounts": []map[string]string{{"name": "accounts/1"}},
		})
	})
	mux.HandleFunc("/accounts/1/locations", func(w http.ResponseWriter, r *http.Request) {
		gotLocationsPath = r.URL.Path + "?" + r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]any{
			"locations": []map[string]string{{"name": "locations/9", "title": "Downtown"}},
		})
	})
	mux.HandleFunc("/locations/9/reviews", func(w http.ResponseWriter, r *http.Request) {
		gotReviewsPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{
			"reviews": []map[string]any{
				{
					"reviewId":   "r1",
					"reviewer":   map[string]any{"displayName": "Jordan T.", "isAnonymous": false},
					"starRating": "FIVE",
					"comment":    "Best coffee in town, and the staff remembered my order!",
					"createTime": "2026-09-11T09:00:00Z",
				},
				{
					"reviewId":   "r2",
					"reviewer":   map[string]any{"isAnonymous": true},
					"starRating": "TWO",
					"comment":    "Waited 25 minutes for a simple order, and it was cold when it arrived.",
					"createTime": "2026-09-05T09:00:00Z",
				},
			},
		})
	})
	c := testClient(t, mux)

	reviews, err := c.ReviewsList("2026-09-10T00:00:00Z")
	if err != nil {
		t.Fatalf("ReviewsList: %v", err)
	}
	if gotLocationsPath != "/accounts/1/locations?readMask=name%2Ctitle" {
		t.Errorf("locations path = %q", gotLocationsPath)
	}
	if gotReviewsPath != "/locations/9/reviews" {
		t.Errorf("reviews path = %q", gotReviewsPath)
	}
	// r2 predates `since` and must be filtered out.
	if len(reviews) != 1 {
		t.Fatalf("reviews = %+v, want exactly r1 (r2 is older than since)", reviews)
	}
	if reviews[0] != (Review{ID: "r1", Author: "Jordan T.", Rating: 5, Text: "Best coffee in town, and the staff remembered my order!"}) {
		t.Errorf("reviews[0] = %+v", reviews[0])
	}
}

func TestReviewsListAnonymousReviewerFallsBackToAnonymous(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"accounts": []map[string]string{{"name": "accounts/1"}}})
	})
	mux.HandleFunc("/accounts/1/locations", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"locations": []map[string]string{{"name": "locations/9"}}})
	})
	mux.HandleFunc("/locations/9/reviews", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"reviews": []map[string]any{
				{"reviewId": "r1", "reviewer": map[string]any{"isAnonymous": true}, "starRating": "THREE",
					"comment": "It was fine.", "createTime": "2026-09-11T09:00:00Z"},
			},
		})
	})
	c := testClient(t, mux)

	reviews, err := c.ReviewsList("")
	if err != nil {
		t.Fatalf("ReviewsList: %v", err)
	}
	if len(reviews) != 1 || reviews[0].Author != "Anonymous" {
		t.Errorf("reviews = %+v, want an anonymous reviewer named \"Anonymous\"", reviews)
	}
}

func TestReviewsListNoAccountsIsAClearError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"accounts": []map[string]string{}})
	})
	c := testClient(t, mux)

	if _, err := c.ReviewsList(""); err == nil {
		t.Error("expected an error when the connection has no Business Profile accounts")
	}
}
