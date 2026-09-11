package google

import "fmt"

var sheetsBase = "https://sheets.googleapis.com/v4/spreadsheets"

// RowsAppend implements form-to-sheet's `rows.append` op. `sheet` is a
// spreadsheet id — unlike Drive's folder paths, Sheets has no name-based
// lookup API, so a bot using this needs the real spreadsheet id, not a
// human-readable name (a real, documented gap, not a missing feature: this
// build's schema.Service has nowhere to declare "resolve this name for me,"
// and adding one is exactly the kind of thing to raise once a second bot
// needs it too).
func (c *Client) RowsAppend(sheetID string, values []any) (rowNumber string, err error) {
	url := fmt.Sprintf("%s/%s/values/A1:append?valueInputOption=USER_ENTERED&insertDataOption=INSERT_ROWS", sheetsBase, sheetID)
	var resp struct {
		Updates struct {
			UpdatedRange string `json:"updatedRange"`
		} `json:"updates"`
	}
	req := map[string]any{"values": [][]any{values}}
	if err := c.postJSON(url, req, &resp); err != nil {
		return "", fmt.Errorf("rows.append: %w", err)
	}
	return resp.Updates.UpdatedRange, nil
}
