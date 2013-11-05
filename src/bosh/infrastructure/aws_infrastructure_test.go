package infrastructure

import (
	"bosh/settings"
	"fmt"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAwsSetupSsh(t *testing.T) {
	expectedKey := "some public key"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, r.Method, "GET")
		assert.Equal(t, r.URL.Path, "/latest/meta-data/public-keys/0/openssh-key")
		w.Write([]byte(expectedKey))
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()

	aws := newAwsInfrastructure(ts.URL, &FakeDnsResolver{})

	fakeSshSetupDelegate := &FakeSshSetupDelegate{}

	err := aws.SetupSsh(fakeSshSetupDelegate, "vcap")
	assert.NoError(t, err)

	assert.Equal(t, fakeSshSetupDelegate.SetupSshPublicKey, expectedKey)
	assert.Equal(t, fakeSshSetupDelegate.SetupSshUsername, "vcap")
}

func TestAwsGetSettingsWhenADnsIsNotProvided(t *testing.T) {
	registryTs, _, expectedSettings := spinUpAwsRegistry(t)
	defer registryTs.Close()

	expectedUserData := fmt.Sprintf(`{"registry":{"endpoint":"%s"}}`, registryTs.URL)

	metadataTs := spinUpAwsMetadaServer(t, expectedUserData)
	defer metadataTs.Close()

	aws := newAwsInfrastructure(metadataTs.URL, &FakeDnsResolver{})

	s, err := aws.GetSettings()
	assert.NoError(t, err)
	assert.Equal(t, s, expectedSettings)
}

func TestAwsGetSettingsWhenDnsServersAreProvided(t *testing.T) {
	fakeDnsResolver := &FakeDnsResolver{
		LookupHostIp: "127.0.0.1",
	}

	registryTs, registryTsPort, expectedSettings := spinUpAwsRegistry(t)
	defer registryTs.Close()

	expectedUserData := fmt.Sprintf(`
		{
			"registry":{
				"endpoint":"http://the.registry.name:%s"
			},
			"dns":{
				"nameserver": ["8.8.8.8", "9.9.9.9"]
			}
		}`,
		registryTsPort)

	metadataTs := spinUpAwsMetadaServer(t, expectedUserData)
	defer metadataTs.Close()

	aws := newAwsInfrastructure(metadataTs.URL, fakeDnsResolver)

	s, err := aws.GetSettings()
	assert.NoError(t, err)
	assert.Equal(t, s, expectedSettings)
	assert.Equal(t, fakeDnsResolver.LookupHostHost, "the.registry.name")
	assert.Equal(t, fakeDnsResolver.LookupHostDnsServers, []string{"8.8.8.8", "9.9.9.9"})
}

func TestAwsSetupNetworking(t *testing.T) {
	fakeDnsResolver := &FakeDnsResolver{}
	aws := newAwsInfrastructure("", fakeDnsResolver)
	fakeDelegate := &FakeNetworkingDelegate{}
	networks := settings.Networks{"bosh": settings.NetworkSettings{}}

	aws.SetupNetworking(fakeDelegate, networks)

	assert.Equal(t, fakeDelegate.SetupDhcpNetworks, networks)
}

// Fake Ssh Setup Delegate

type FakeSshSetupDelegate struct {
	SetupSshPublicKey string
	SetupSshUsername  string
}

func (d *FakeSshSetupDelegate) SetupSsh(publicKey, username string) (err error) {
	d.SetupSshPublicKey = publicKey
	d.SetupSshUsername = username
	return
}

// Fake Networking Delegate

type FakeNetworkingDelegate struct {
	SetupDhcpNetworks settings.Networks
}

func (d *FakeNetworkingDelegate) SetupDhcp(networks settings.Networks) (err error) {
	d.SetupDhcpNetworks = networks
	return
}

// Fake Dns Resolver

type FakeDnsResolver struct {
	LookupHostIp         string
	LookupHostDnsServers []string
	LookupHostHost       string
}

func (res *FakeDnsResolver) LookupHost(dnsServers []string, host string) (ip string, err error) {
	res.LookupHostDnsServers = dnsServers
	res.LookupHostHost = host
	ip = res.LookupHostIp
	return
}

// Server methods

func spinUpAwsRegistry(t *testing.T) (ts *httptest.Server, port string, expectedSettings settings.Settings) {
	settingsJson := `{
		"agent_id": "my-agent-id",
		"networks": {
			"netA": {
				"default": ["dns", "gateway"],
				"dns": [
					"xx.xx.xx.xx",
					"yy.yy.yy.yy"
				]
			},
			"netB": {
				"dns": [
					"zz.zz.zz.zz"
				]
			}
		},
		"mbus": "https://vcap:b00tstrap@0.0.0.0:6868"
	}`
	settingsJson = strings.Replace(settingsJson, `"`, `\"`, -1)
	settingsJson = strings.Replace(settingsJson, "\n", "", -1)
	settingsJson = strings.Replace(settingsJson, "\t", "", -1)

	settingsJson = fmt.Sprintf(`{"settings": "%s"}`, settingsJson)

	expectedSettings = settings.Settings{
		AgentId: "my-agent-id",
		Networks: settings.Networks{
			"netA": settings.NetworkSettings{
				Default: []string{"dns", "gateway"},
				Dns:     []string{"xx.xx.xx.xx", "yy.yy.yy.yy"},
			},
			"netB": settings.NetworkSettings{
				Dns: []string{"zz.zz.zz.zz"},
			},
		},
		Mbus: "https://vcap:b00tstrap@0.0.0.0:6868",
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, r.Method, "GET")
		assert.Equal(t, r.URL.Path, "/instances/123-456-789/settings")
		w.Write([]byte(settingsJson))
	})

	ts = httptest.NewServer(handler)

	registryUrl, err := url.Parse(ts.URL)
	assert.NoError(t, err)
	port = strings.Split(registryUrl.Host, ":")[1]

	return
}

func spinUpAwsMetadaServer(t *testing.T, userData string) (ts *httptest.Server) {
	instanceId := "123-456-789"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, r.Method, "GET")

		switch r.URL.Path {
		case "/latest/user-data":
			w.Write([]byte(userData))
		case "/latest/meta-data/instance-id":
			w.Write([]byte(instanceId))
		}
	})

	ts = httptest.NewServer(handler)
	return
}
