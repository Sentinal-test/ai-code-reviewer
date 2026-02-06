package db

type Client struct{}

func (c *Client) EmailExists(email string) bool {
	// Logic to check global uniqueness
	return false
}
