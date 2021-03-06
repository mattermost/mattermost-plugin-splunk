package plugin

import (
	"math/rand"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/mattermost/mattermost-server/v5/model"
	mattermostPlugin "github.com/mattermost/mattermost-server/v5/plugin"

	"github.com/bakurits/mattermost-plugin-splunk/server/api"
	"github.com/bakurits/mattermost-plugin-splunk/server/command"
	"github.com/bakurits/mattermost-plugin-splunk/server/config"
	"github.com/bakurits/mattermost-plugin-splunk/server/splunk"
	"github.com/bakurits/mattermost-plugin-splunk/server/store"

	"github.com/pkg/errors"
)

// Plugin is interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin interface {
	splunk.PluginAPI
	OnActivate() error
	OnConfigurationChange() error
	ServeHTTP(pc *mattermostPlugin.Context, w http.ResponseWriter, r *http.Request)
}

// NewWithConfig creates new plugin object from configuration
func NewWithConfig(conf *config.Config) Plugin {
	p := &plugin{
		configurationLock: &sync.RWMutex{},
		config:            conf,
	}
	return p
}

// NewWithStore creates new plugin object from configuration and store object
func NewWithStore(store store.Store, conf *config.Config) Plugin {
	p := &plugin{
		configurationLock: &sync.RWMutex{},
		config:            conf,
	}

	p.sp = splunk.New(p, store)
	p.httpHandler = api.NewHTTPHandler(p.sp)
	return p
}

// NewWithSplunk creates new plugin object from splunk
func NewWithSplunk(sp splunk.Splunk, conf *config.Config) Plugin {
	p := &plugin{
		configurationLock: &sync.RWMutex{},
		config:            conf,
		sp:                sp,
	}

	p.httpHandler = api.NewHTTPHandler(p.sp)
	return p
}

// OnActivate called when plugin is activated
func (p *plugin) OnActivate() error {
	rand.Seed(time.Now().UnixNano())

	if p.sp == nil {
		pluginStore := store.NewPluginStore(p)
		p.sp = splunk.New(p, pluginStore)
		p.httpHandler = api.NewHTTPHandler(p.sp)
	}

	err := p.API.RegisterCommand(command.GetSlashCommand())
	if err != nil {
		return errors.Wrap(err, "OnActivate: failed to register command")
	}

	botID, _ := p.Helpers.EnsureBot(&model.Bot{
		Username:    "splunk",
		DisplayName: "Splunk",
		Description: "Created by the Splunk plugin.",
	})
	p.sp.AddBotUser(botID)

	return nil
}

// ExecuteCommand hook is called when slash command is submitted
func (p *plugin) ExecuteCommand(_ *mattermostPlugin.Context, commandArgs *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	mattermostUserID := commandArgs.UserId
	if len(mattermostUserID) == 0 {
		return &model.CommandResponse{}, &model.AppError{Message: "Not authorized"}
	}

	commandHandler := command.NewHandler(commandArgs, p.GetConfiguration(), p.sp)
	args := strings.Fields(commandArgs.Command)

	commandResponse, err := commandHandler.Handle(args...)
	if err == nil {
		return commandResponse, nil
	}

	if appError, ok := err.(*model.AppError); ok {
		return commandResponse, appError
	}

	return commandResponse, &model.AppError{
		Message: err.Error(),
	}
}

// OnConfigurationChange is invoked when Config changes may have been made.
func (p *plugin) OnConfigurationChange() error {
	var configuration = new(config.Config)

	// Load the public Config fields from the Mattermost server Config.
	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return errors.Wrap(err, "failed to load plugin Config")
	}

	p.setConfiguration(configuration)

	return nil
}

func (p *plugin) ServeHTTP(_ *mattermostPlugin.Context, w http.ResponseWriter, req *http.Request) {
	p.httpHandler.ServeHTTP(w, req)
}

// GetConfiguration retrieves the active Config under lock, making it safe to use
// concurrently. The active Config may change underneath the client of this method, but
// the struct returned by this API call is considered immutable.
func (p *plugin) GetConfiguration() *config.Config {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.config == nil {
		return &config.Config{}
	}

	return p.config
}

type plugin struct {
	mattermostPlugin.MattermostPlugin

	httpHandler http.Handler

	sp splunk.Splunk

	// configurationLock synchronizes access to the configuration.
	configurationLock *sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	config *config.Config
}

// setConfiguration replaces the active Config under lock.
//
// Do not call setConfiguration while holding the configurationLock, as sync.Mutex is not
// re-entrant. In particular, avoid using the plugin API entirely, as this may in turn trigger a
// hook back into the plugin. If that hook attempts to acquire this lock, a deadlock may occur.
//
// This method panics if setConfiguration is called with the existing Config. This almost
// certainly means that the Config was modified without being cloned and may result in
// an unsafe access.
func (p *plugin) setConfiguration(configuration *config.Config) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.config == configuration {
		// Ignore assignment if the Config struct is empty. Go will optimize the
		// allocation for same to point at the same memory address, breaking the check
		// above.
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}

		panic("setConfiguration called with the existing Config")
	}

	p.config = configuration
}
