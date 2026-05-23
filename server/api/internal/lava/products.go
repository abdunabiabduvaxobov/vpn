package lava

import (
	"context"
	"fmt"
)

// ListProducts calls GET /api/v2/products and follows the `nextPage` cursor
// server-side until exhausted. Returns the flattened list of products. The
// admin proxy endpoint (plan 03-05 / D-12) normalizes this into a flat
// {productId, offerId, periodicity, currency, amount} dropdown source.
func (c *Client) ListProducts(ctx context.Context) ([]ProductItemResponse, error) {
	var all []ProductItemResponse
	next := ""
	for {
		path := "/api/v2/products"
		if next != "" {
			path += "?nextPage=" + escapeQuery(next)
		}
		resp, err := c.do(ctx, "GET", path, nil)
		if err != nil {
			return nil, fmt.Errorf("lava ListProducts: %w", err)
		}
		var page ProductsResponse
		if err := decodeJSON(resp, &page); err != nil {
			return nil, fmt.Errorf("lava ListProducts: %w", err)
		}
		for _, item := range page.Items {
			if item.Type == "PRODUCT" {
				all = append(all, item.Data)
			}
		}
		if page.NextPage == nil || *page.NextPage == "" {
			break
		}
		next = *page.NextPage
	}
	return all, nil
}
