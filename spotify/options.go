package spotify

// Option is a function that configures a Client
type Option func(*Client) error

// WithMessenger configures the client with a message poster
func WithMessenger(messenger MessagePoster) Option {
	return func(c *Client) error {
		c.messenger = messenger
		return nil
	}
}
