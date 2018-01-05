package conjurapi

import (
  "fmt"
  "io"
  "net/http"
  "net/url"
  "strings"

  "github.com/cyberark/conjur-api-go/conjurapi/authn"
)

type RouterV5 struct {
  Config *Config
}

func (self RouterV5) AuthenticateRequest(loginPair authn.LoginPair) (*http.Request, error) {
  authenticateUrl := fmt.Sprintf("%s/authn/%s/%s/authenticate", self.Config.ApplianceURL, self.Config.Account, url.QueryEscape(loginPair.Login))

  req, err := http.NewRequest("POST", authenticateUrl, strings.NewReader(loginPair.APIKey))
  if err != nil {
    return nil, err
  }
  req.Header.Set("Content-Type", "text/plain")

  return req, nil
}

func (self RouterV5) LoadPolicyRequest(policyId string, policy io.Reader) (*http.Request, error) {
	policyId = makeFullId(self.Config.Account, "policy", policyId)

  tokens := strings.SplitN(policyId, ":", 3)
  policyUrl := fmt.Sprintf("%s/policies/%s/%s/%s", self.Config.ApplianceURL, tokens[0], tokens[1], url.QueryEscape(tokens[2]))

  return http.NewRequest(
    "PUT",
    policyUrl,
    policy,
  )
}

func (self RouterV5) RetrieveSecretRequest(variableId string) (*http.Request, error) {
	variableId = makeFullId(self.Config.Account, "variable", variableId)

  return http.NewRequest(
    "GET",
    self.variableURL(variableId),
    nil,
  )
}

func (self RouterV5) AddSecretRequest(variableId, secretValue string) (*http.Request, error) {
	variableId = makeFullId(self.Config.Account, "variable", variableId)

  return http.NewRequest(
    "POST",
    self.variableURL(variableId),
    strings.NewReader(secretValue),
  )
}

func (self RouterV5) variableURL(variableId string) string {
  tokens := strings.SplitN(variableId, ":", 3)
  return fmt.Sprintf("%s/secrets/%s/%s/%s", self.Config.ApplianceURL, tokens[0], tokens[1], url.QueryEscape(tokens[2]))
}
