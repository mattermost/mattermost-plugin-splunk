package splunk

import (
	"encoding/xml"
	"log"
	"net/http"
	"strings"

	"github.com/mattermost/mattermost-plugin-splunk/server/store"

	"github.com/mattermost/mattermost-server/v6/model"
	"github.com/pkg/errors"
)

// Splunk API for business logic
type Splunk interface {
	PluginAPI

	User() store.SplunkUser
	SyncUser(mattermostUserID string) error
	LoginUser(mattermostUserID string, server string, id string) error
	LogoutUser(mattermostUserID string) error

	AddAlert(string, string) error
	Notify(string, AlertActionWHPayload) error
	ListAlert(string) ([]string, error)
	DeleteAlert(string, string) error

	AddBotUser(string)
	BotUser() string

	Logs(string) (LogResults, error)
	ListLogs() []string
}

// check if the interface implements all methods
var _ Splunk = (*splunk)(nil)

// PluginAPI API form mattermost plugin
type PluginAPI interface {
	SendEphemeralPost(userID string, post *model.Post) *model.Post
	CreatePost(post *model.Post) (*model.Post, error)

	GetUsersInChannel(channelID, sortBy string, page, perPage int) ([]*model.User, error)
	PublishWebSocketEvent(event string, payload map[string]interface{}, broadcast *model.WebsocketBroadcast)
	store.API
}

type splunk struct {
	PluginAPI
	store.Store
	botUserID string

	currentUser store.SplunkUser

	httpClient *http.Client
}

// New returns new Splunk API object
func New(api PluginAPI, st store.Store) Splunk {
	return newSplunk(api, st)
}

// AddBotUser registers new bot user
func (s *splunk) AddBotUser(bID string) {
	s.botUserID = bID
}

// BotUser returns id of bot user
func (s *splunk) BotUser() string {
	return s.botUserID
}

// User returns splunk user info
func (s *splunk) User() store.SplunkUser {
	return s.currentUser
}

type currentUserResponse struct {
	Data []struct {
		Data string `xml:",chardata"`
		Name string `xml:"name,attr"`
	} `xml:"entry>content>dict>key"`
}

func (s *splunk) authCheck() error {
	resp, err := s.doHTTPRequest(http.MethodGet, "/services/authentication/current-context", nil)
	if err != nil {
		return errors.Wrap(err, "authorization")
	}
	defer func() { _ = resp.Body.Close() }()
	var c currentUserResponse
	if err = xml.NewDecoder(resp.Body).Decode(&c); err != nil {
		log.Println(err)
		return errors.Wrap(err, "authorization")
	}
	for _, r := range c.Data {
		if r.Name == "username" {
			s.currentUser.UserName = r.Data
		}
	}

	if s.currentUser.UserName == "" {
		return errors.New("authorization")
	}
	return nil
}

// SyncUser syncs user stored in KVStore with user stored in memory.
func (s *splunk) SyncUser(mattermostUserID string) error {
	u, err := s.Store.CurrentUser(mattermostUserID)
	if err != nil {
		return err
	}

	s.currentUser = u
	return nil
}

// LoginUser changes authorized user.
// id is either username or username/token of user.
func (s *splunk) LoginUser(mattermostUserID string, server string, id string) error {
	var isNew = true

	// id can be username or username/token
	username, token, err := extractUserInfo(id)
	if err != nil {
		return err
	}

	// check if we already have token for given username
	if u, err := s.Store.User(mattermostUserID, server, username); err == nil {
		s.currentUser = u
		isNew = false
	} else {
		if token == "" {
			return errors.New("The token is empty to authenticate a user")
		}

		s.currentUser = store.SplunkUser{
			Server: server,
			Token:  token,
		}
	}

	if authErr := s.authCheck(); authErr != nil {
		s.currentUser = store.SplunkUser{}
		return authErr
	}

	if isNew {
		return s.Store.RegisterUser(mattermostUserID, s.currentUser)
	}

	return s.Store.ChangeCurrentUser(mattermostUserID, s.currentUser.UserName)
}

// LogoutUser logs user out.
func (s *splunk) LogoutUser(mattermostUserID string) error {
	_ = s.Store.ChangeCurrentUser(mattermostUserID, "")
	err := s.Store.DeleteUser(mattermostUserID, s.currentUser.Server, s.currentUser.UserName)
	s.currentUser = store.SplunkUser{}
	return err
}

func newSplunk(api PluginAPI, st store.Store) *splunk {
	s := &splunk{
		PluginAPI:  api,
		Store:      st,
		httpClient: http.DefaultClient,
	}

	return s
}

func extractUserInfo(id string) (string, string, error) {
	if id == "" {
		return "", "", errors.New("Please provide username and token like so: username/token. You can user username only if already authenticated")
	}

	loginData := strings.Split(id, "/")
	username := loginData[0]
	token := ""

	if len(loginData) > 2 {
		return "", "", errors.New("Arguments to extract username and/or token must be 2")
	} else if len(loginData) == 2 {
		token = loginData[1]
	}

	return username, token, nil
}
