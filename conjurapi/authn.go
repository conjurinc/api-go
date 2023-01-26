package conjurapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/cyberark/conjur-api-go/conjurapi/authn"
	"github.com/cyberark/conjur-api-go/conjurapi/logging"
	"github.com/cyberark/conjur-api-go/conjurapi/response"
)

// OidcProvider contains information about an OIDC provider.
type OidcProvider struct {
	ServiceID    string `json:"service_id"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
}

func (c *Client) RefreshToken() (err error) {
	// Fetch cached conjur access token if using OIDC
	if c.GetConfig().AuthnType == "oidc" {
		token := c.readCachedAccessToken()
		if token != nil {
			c.authToken = token
		}
	}

	if c.NeedsTokenRefresh() {
		return c.refreshToken()
	}

	return nil
}

func (c *Client) ForceRefreshToken() error {
	return c.refreshToken()
}

func (c *Client) refreshToken() error {
	var tokenBytes []byte
	tokenBytes, err := c.authenticator.RefreshToken()
	if err != nil {
		return err
	}

	token, err := authn.NewToken(tokenBytes)
	if err != nil {
		return err
	}

	token.FromJSON(tokenBytes)
	c.authToken = token
	return nil
}

func (c *Client) NeedsTokenRefresh() bool {
	return c.authToken == nil ||
		c.authToken.ShouldRefresh() ||
		c.authenticator.NeedsTokenRefresh()
}

func (c *Client) readCachedAccessToken() *authn.AuthnToken {
	tokenBytes, err := c.storage.ReadAuthnToken()
	if err != nil {
		return nil
	}

	token, err := authn.NewToken(tokenBytes)
	if err != nil {
		return nil
	}

	token.FromJSON(token.Raw())
	return token
}

func (c *Client) createAuthRequest(req *http.Request) error {
	if err := c.RefreshToken(); err != nil {
		return err
	}

	req.Header.Set(
		"Authorization",
		fmt.Sprintf("Token token=\"%s\"", base64.StdEncoding.EncodeToString(c.authToken.Raw())),
	)

	return nil
}

// ChangeCurrentUserPassword changes the password of the currently authenticated user
func (c *Client) ChangeCurrentUserPassword(password string, newPassword string) ([]byte, error) {
	whoamiResponse, err := c.WhoAmI()
	if err != nil {
		return nil, err
	}

	roleType, roleName, _ := whoamiResponse.Role()
	if roleType != "user" {
		return nil, fmt.Errorf("password can only be changed for role of type 'user': got type %q", roleType)
	}

	req, err := c.ChangeUserPasswordRequest(roleName, password, newPassword)
	if err != nil {
		return nil, err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return response.DataResponse(res)
}

func (c *Client) ChangeUserPassword(username string, password string, newPassword string) ([]byte, error) {
	req, err := c.ChangeUserPasswordRequest(username, password, newPassword)
	if err != nil {
		return nil, err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return response.DataResponse(res)
}

// Login exchanges a user's password for an API key.
func (c *Client) Login(login string, password string) ([]byte, error) {
	req, err := c.LoginRequest(login, password)
	if err != nil {
		return nil, err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	apiKey, err := response.DataResponse(res)
	if err != nil {
		return nil, err
	}

	// Store the API key in the credentials store
	if c.storage != nil {
		err = c.storage.StoreCredentials(login, string(apiKey))
	}
	return apiKey, err
}

// PurgeCredentials purges credentials from the client's credential storage.
func (c *Client) PurgeCredentials() error {
	if c.storage == nil {
		return nil
	}

	return c.storage.PurgeCredentials()
}

// PurgeCredentials purges credentials from the credential storage indicated by the
// configuration.
func PurgeCredentials(config Config) error {
	storage, err := createStorageProvider(config)
	if err != nil {
		return err
	}

	if storage == nil {
		logging.ApiLog.Debugf("Not storing credentials, so nothing to purge")
		return nil
	}

	return storage.PurgeCredentials()
}

// Authenticate obtains a new access token using the internal authenticator.
func (c *Client) InternalAuthenticate() ([]byte, error) {
	if c.authenticator == nil {
		return nil, errors.New("unable to authenticate using client without authenticator")
	}

	// If using OIDC, check if we have a cached access token
	if c.GetConfig().AuthnType == "oidc" {
		token := c.readCachedAccessToken()
		if token != nil && !token.ShouldRefresh() {
			return token.Raw(), nil
		} else {
			// We can't simply refresh the token because it'll require user input. Instead,
			// we return an error and inform the client/user to login again.
			return nil, errors.New("No valid OIDC token found. Please login again.")
		}
	}

	// Otherwise refresh the token
	return c.authenticator.RefreshToken()
}

// WhoamiResponse represents metadata on the currently authenticated user
type WhoamiResponse struct {
	ClientIP      string `json:"client_ip"`
	UserAgent     string `json:"user_agent"`
	Account       string `json:"account"`
	Username      string `json:"username"`
	TokenIssuedAt string `json:"token_issued_at"`
}

// Role returns the role type, role name, and role id respectively using values on WhoamiResponse
func (w WhoamiResponse) Role() (string, string, string) {
	return roleFromUsername(w.Account, w.Username)
}

// WhoAmI obtains information on the current user.
func (c *Client) WhoAmI() (WhoamiResponse, error) {
	var whoamiResponse WhoamiResponse

	req, err := c.WhoAmIRequest()
	if err != nil {
		return whoamiResponse, err
	}

	res, err := c.SubmitRequest(req)
	if err != nil {
		return whoamiResponse, err
	}

	whoAmIData, err := response.DataResponse(res)
	if err != nil {
		return whoamiResponse, err
	}

	err = json.Unmarshal(whoAmIData, &whoamiResponse)

	return whoamiResponse, err
}

// Authenticate obtains a new access token.
func (c *Client) Authenticate(loginPair authn.LoginPair) ([]byte, error) {
	resp, err := c.authenticate(loginPair)
	if err != nil {
		return nil, err
	}

	return response.DataResponse(resp)
}

// AuthenticateReader obtains a new access token and returns it as a data stream.
func (c *Client) AuthenticateReader(loginPair authn.LoginPair) (io.ReadCloser, error) {
	resp, err := c.authenticate(loginPair)
	if err != nil {
		return nil, err
	}

	return response.SecretDataResponse(resp)
}

func (c *Client) authenticate(loginPair authn.LoginPair) (*http.Response, error) {
	req, err := c.AuthenticateRequest(loginPair)
	if err != nil {
		return nil, err
	}

	return c.httpClient.Do(req)
}

func (c *Client) OidcAuthenticate(code, nonce, code_verifier string) ([]byte, error) {
	req, err := c.OidcAuthenticateRequest(code, nonce, code_verifier)
	if err != nil {
		return nil, err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	resp, err := response.DataResponse(res)

	if err == nil && c.storage != nil {
		c.storage.StoreAuthnToken(resp)
	}

	return resp, err
}

func (c *Client) ListOidcProviders() ([]OidcProvider, error) {
	req, err := c.ListOidcProvidersRequest()
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	providers := []OidcProvider{}
	err = response.JSONResponse(resp, &providers)

	return providers, err
}

// RotateAPIKey replaces the API key of a role on the server with a new
// random secret.
//
// The authenticated user must have update privilege on the role.
func (c *Client) RotateAPIKey(roleID string) ([]byte, error) {
	resp, err := c.rotateAPIKey(roleID)
	if err != nil {
		return nil, err
	}

	return response.DataResponse(resp)
}

// RotateUserAPIKey constructs a role ID from a given user ID then replaces the
// API key of the role with a new random secret.
//
// The authenticated user must have update privilege on the role.
func (c *Client) RotateUserAPIKey(userID string) ([]byte, error) {
	config := c.GetConfig()
	roleID := fmt.Sprintf("%s:user:%s", config.Account, userID)
	return c.RotateAPIKey(roleID)
}

// RotateCurrentRoleAPIKey rotates the
// API key of the currently authenticated role with a new random secret.
func (c *Client) RotateCurrentRoleAPIKey() ([]byte, error) {
	whoamiResponse, err := c.WhoAmI()
	if err != nil {
		return nil, err
	}

	_, _, roleId := whoamiResponse.Role()
	return c.RotateAPIKey(roleId)
}

// RotateHostAPIKey constructs a role ID from a given host ID then replaces the
// API key of the role with a new random secret.
//
// The authenticated user must have update privilege on the role.
func (c *Client) RotateHostAPIKey(hostID string) ([]byte, error) {
	config := c.GetConfig()
	roleID := fmt.Sprintf("%s:host:%s", config.Account, hostID)

	return c.RotateAPIKey(roleID)
}

// RotateAPIKeyReader replaces the API key of a role on the server with a new
// random secret and returns it as a data stream.
//
// The authenticated user must have update privilege on the role.
func (c *Client) RotateAPIKeyReader(roleID string) (io.ReadCloser, error) {
	resp, err := c.rotateAPIKey(roleID)
	if err != nil {
		return nil, err
	}

	return response.SecretDataResponse(resp)
}

func (c *Client) rotateAPIKey(roleID string) (*http.Response, error) {
	req, err := c.RotateAPIKeyRequest(roleID)
	if err != nil {
		return nil, err
	}

	return c.SubmitRequest(req)
}
