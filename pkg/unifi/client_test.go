package unifi_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ljfranklin/port-forwarding-controller/pkg/forwarding"
	"github.com/ljfranklin/port-forwarding-controller/pkg/unifi"
	. "github.com/onsi/gomega"
)

type testServer struct {
	t                     *testing.T
	siteName              string
	customLoginHandler    http.Handler
	customListHandler     http.Handler
	customCreateHandler   http.Handler
	customDeleteHandler   http.Handler
	lastCreateRequestBody []byte
	deleteCallCount       int
}

func (s *testServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g := NewGomegaWithT(s.t)

	if s.siteName == "" {
		s.siteName = "default"
	}

	if r.URL.Path != "/api/login" {
		if _, err := r.Cookie("some-cookie"); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"data": [] ,"meta": {"msg": "api.err.LoginRequired", "rc": "error"}}`))
			return
		}
	}

	switch r.URL.Path {
	case "/api/login":
		if s.customLoginHandler != nil {
			s.customLoginHandler.ServeHTTP(w, r)
			return
		}

		body, err := ioutil.ReadAll(r.Body)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(string(body)).To(MatchJSON(`{"username": "some-user", "password": "some-password"}`))

		http.SetCookie(w, &http.Cookie{
			Name:  "some-cookie",
			Value: "some-value",
		})
		w.Write([]byte(`{"data": [] ,"meta": {"rc": "ok"}}`))
	case fmt.Sprintf("/api/s/%s/rest/portforward", s.siteName):
		switch r.Method {
		case "GET":
			if s.customListHandler != nil {
				s.customListHandler.ServeHTTP(w, r)
				return
			}

			c, err := r.Cookie("some-cookie")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Value).To(Equal("some-value"))

			w.Write([]byte(`{
				"data": [
					{
						"_id": "5bd919f20889ae0019309113",
						"dst_port": "80",
						"enabled": true,
						"fwd": "1.2.3.4",
						"fwd_port": "80",
						"name": "name-1",
						"proto": "tcp_udp",
						"site_id": "5bd85ec40889ae0019308fbe",
						"src": "any"
					},
					{
						"_id": "5bd91a040889ae0019309114",
						"dst_port": "443",
						"enabled": true,
						"fwd": "5.6.7.8",
						"fwd_port": "443",
						"name": "name-2",
						"proto": "tcp_udp",
						"site_id": "5bd85ec40889ae0019308fbe",
						"src": "10.0.0.0/16"
					}
				],
				"meta": {
					"rc": "ok"
				}
			}`))
		case "POST":
			if s.customCreateHandler != nil {
				s.customCreateHandler.ServeHTTP(w, r)
				return
			}

			c, err := r.Cookie("some-cookie")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Value).To(Equal("some-value"))

			s.lastCreateRequestBody, err = ioutil.ReadAll(r.Body)
			g.Expect(err).NotTo(HaveOccurred())
			defer r.Body.Close()

			w.Write([]byte(`{}`))
		default:
			s.t.Errorf("unexpected request method %s to %s", r.Method, r.URL.Path)
		}
	case fmt.Sprintf("/api/s/%s/rest/portforward/5bd919f20889ae0019309113", s.siteName):
		switch r.Method {
		case "DELETE":
			if s.customDeleteHandler != nil {
				s.customDeleteHandler.ServeHTTP(w, r)
				return
			}

			c, err := r.Cookie("some-cookie")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Value).To(Equal("some-value"))

			s.deleteCallCount++

			w.Write([]byte(`{}`))
		default:
			s.t.Errorf("unexpected request method %s to %s", r.Method, r.URL.Path)
		}
	default:
		s.t.Errorf("unexpected request to %s", r.URL.Path)
	}
}

func TestListAddresses(t *testing.T) {
	g := NewGomegaWithT(t)

	ts := httptest.NewTLSServer(&testServer{t: t})
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	addresses, err := client.ListAddresses(nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(addresses).To(Equal([]forwarding.Address{
		{
			Name: "name-1",
			Port: 80,
			IP:   "1.2.3.4",
		},
		{
			Name:        "name-2",
			Port:        443,
			IP:          "5.6.7.8",
			SourceRange: "10.0.0.0/16",
		},
	}))
}

func TestListAddressesWithUnifiSiteOption(t *testing.T) {
	g := NewGomegaWithT(t)

	ts := httptest.NewTLSServer(&testServer{t: t, siteName: "some-site"})
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	addresses, err := client.ListAddresses(map[string]string{
		"unifi-site": "some-site",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(addresses).To(Equal([]forwarding.Address{
		{
			Name: "name-1",
			Port: 80,
			IP:   "1.2.3.4",
			Options: map[string]string{
				"unifi-site": "some-site",
			},
		},
		{
			Name:        "name-2",
			Port:        443,
			IP:          "5.6.7.8",
			SourceRange: "10.0.0.0/16",
			Options: map[string]string{
				"unifi-site": "some-site",
			},
		},
	}))
}

func TestBadLogin(t *testing.T) {
	g := NewGomegaWithT(t)

	badLogin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{ "data": [], "meta" : { "msg": "api.err.SomeError", "rc": "error"}}`))
	})
	ts := httptest.NewTLSServer(&testServer{t: t, customLoginHandler: badLogin})
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	_, err := client.ListAddresses(nil)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("api.err.SomeError"))
	g.Expect(err.Error()).To(ContainSubstring("401"))
}

func TestListAddressesWithBadRespCode(t *testing.T) {
	g := NewGomegaWithT(t)

	badList := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ts := httptest.NewTLSServer(&testServer{t: t, customListHandler: badList})
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	_, err := client.ListAddresses(nil)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("500"))
}

func TestCreateAddAddress(t *testing.T) {
	g := NewGomegaWithT(t)

	fakeServer := &testServer{t: t}
	ts := httptest.NewTLSServer(fakeServer)
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	err := client.CreateAddress(forwarding.Address{
		Name: "name-1",
		Port: 80,
		IP:   "1.2.3.4",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fakeServer.lastCreateRequestBody).To(MatchJSON(`
	{
		"dst_port":	"80",
		"enabled":	true,
		"fwd": "1.2.3.4",
		"fwd_port": "80",
		"name": "name-1",
		"proto": "tcp_udp",
		"src": "any"
	}
`))
}

func TestCreateAddAddressWithSourceRange(t *testing.T) {
	g := NewGomegaWithT(t)

	fakeServer := &testServer{t: t}
	ts := httptest.NewTLSServer(fakeServer)
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	err := client.CreateAddress(forwarding.Address{
		Name:        "name-1",
		Port:        80,
		IP:          "1.2.3.4",
		SourceRange: "10.0.0.0/16",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fakeServer.lastCreateRequestBody).To(MatchJSON(`
	{
		"dst_port":	"80",
		"enabled":	true,
		"fwd": "1.2.3.4",
		"fwd_port": "80",
		"name": "name-1",
		"proto": "tcp_udp",
		"src": "10.0.0.0/16"
	}
`))
}

func TestCreateAddAddressWithUnifiSiteOption(t *testing.T) {
	g := NewGomegaWithT(t)

	fakeServer := &testServer{t: t, siteName: "some-site"}
	ts := httptest.NewTLSServer(fakeServer)
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	err := client.CreateAddress(forwarding.Address{
		Name: "name-1",
		Port: 80,
		IP:   "1.2.3.4",
		Options: map[string]string{
			"unifi-site": "some-site",
		},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fakeServer.lastCreateRequestBody).To(MatchJSON(`
	{
		"dst_port":	"80",
		"enabled":	true,
		"fwd": "1.2.3.4",
		"fwd_port": "80",
		"name": "name-1",
		"proto": "tcp_udp",
		"src": "any"
	}
`))
}

func TestCreateAddAddressWithBadCode(t *testing.T) {
	g := NewGomegaWithT(t)

	badCreate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ts := httptest.NewTLSServer(&testServer{t: t, customCreateHandler: badCreate})
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	err := client.CreateAddress(forwarding.Address{
		Name: "name-1",
		Port: 80,
		IP:   "1.2.3.4",
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("500"))
}

func TestDeleteAddress(t *testing.T) {
	g := NewGomegaWithT(t)

	testServer := &testServer{t: t}
	ts := httptest.NewTLSServer(testServer)
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	err := client.DeleteAddress(forwarding.Address{
		Name: "name-1",
		Port: 80,
		IP:   "1.2.3.4",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testServer.deleteCallCount).To(Equal(1))
}

func TestDeleteAddressThatDoesNotExist(t *testing.T) {
	g := NewGomegaWithT(t)

	testServer := &testServer{t: t}
	ts := httptest.NewTLSServer(testServer)
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	err := client.DeleteAddress(forwarding.Address{
		Name: "does-not-exist",
		Port: 80,
		IP:   "1.2.3.4",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testServer.deleteCallCount).To(Equal(0))
}

func TestDeleteAddressWithUnifiSiteOption(t *testing.T) {
	g := NewGomegaWithT(t)

	testServer := &testServer{t: t, siteName: "some-site"}
	ts := httptest.NewTLSServer(testServer)
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	err := client.DeleteAddress(forwarding.Address{
		Name: "name-1",
		Port: 80,
		IP:   "1.2.3.4",
		Options: map[string]string{
			"unifi-site": "some-site",
		},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testServer.deleteCallCount).To(Equal(1))
}

func TestDeleteAddAddressWithBadCode(t *testing.T) {
	g := NewGomegaWithT(t)

	badDelete := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ts := httptest.NewTLSServer(&testServer{t: t, customDeleteHandler: badDelete})
	defer ts.Close()

	testClient := ts.Client()
	client := unifi.Client{
		HTTPClient:    testClient,
		ControllerURL: ts.URL,
		Username:      "some-user",
		Password:      "some-password",
	}

	err := client.DeleteAddress(forwarding.Address{
		Name: "name-1",
		Port: 80,
		IP:   "1.2.3.4",
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("500"))
}
