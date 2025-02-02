package api

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/mattermost/mattermost-marketplace/internal/model"
	"github.com/pkg/errors"
)

// Client is the programmatic interface to the marketplace server API.
type Client struct {
	Address    string
	httpClient *http.Client
}

// NewClient creates a client to the marketplace server at the given address.
func NewClient(address string) *Client {
	return &Client{
		Address:    address,
		httpClient: &http.Client{},
	}
}

// closeBody ensures the Body of an http.Response is properly closed.
func closeBody(r *http.Response) {
	if r.Body != nil {
		_, _ = ioutil.ReadAll(r.Body)
		_ = r.Body.Close()
	}
}

func (c *Client) buildURL(urlPath string, args ...interface{}) string {
	return fmt.Sprintf("%s%s", c.Address, fmt.Sprintf(urlPath, args...))
}

func (c *Client) doGet(u string) (*http.Response, error) {
	return c.httpClient.Get(u)
}

// GetPlugins fetches the list of plugins from the configured server.
func (c *Client) GetPlugins(request *GetPluginsRequest) ([]*model.Plugin, error) {
	u, err := url.Parse(c.buildURL("/api/v1/plugins"))
	if err != nil {
		return nil, err
	}

	request.ApplyToURL(u)

	resp, err := c.doGet(u.String())
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return model.PluginsFromReader(resp.Body)
	default:
		return nil, errors.Errorf("failed with status code %d", resp.StatusCode)
	}
}
