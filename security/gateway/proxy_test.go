package gateway_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/CS-SI/SafeScale/security/model"
	oidc "github.com/coreos/go-oidc"
	"golang.org/x/oauth2"

	"github.com/CS-SI/SafeScale/security/gateway"
	"github.com/stretchr/testify/assert"
)

func Clean() {
	db := model.NewDataAccess("sqlite3", "/tmp/safe-security.db").Get()
	defer db.Close()
	db.DropTableIfExists(&model.Service{}, &model.Role{}, &model.AccessPermission{}, &model.User{})
}
func runTestService() {
	da := model.NewDataAccess("sqlite3", "/tmp/safe-security.db")
	db := da.Get().Debug()
	defer db.Close()
	db.AutoMigrate(&model.Service{}, &model.Role{}, &model.AccessPermission{}, &model.User{})

	srv1 := model.Service{
		BaseURL: "http://localhost:10000/date",
		Name:    "TEST",
	}
	if err := db.Create(&srv1).Error; err != nil {
		log.Fatal()
	}

	usr1 := model.User{
		Email: "user@c-s.fr",
	}
	if err := db.Create(&usr1).Error; err != nil {
		log.Fatal(err)
	}
	perm1 := model.AccessPermission{
		Action:          "GET",
		ResourcePattern: "*",
	}

	if err := db.Create(&perm1).Error; err != nil {
		log.Fatal(err)
	}

	role1 := model.Role{
		Name: "USER",
	}

	if err := db.Create(&role1).Error; err != nil {
		log.Fatal(err)
	}
	if err := db.Model(&role1).Association("AccessPermissions").Append(perm1).Error; err != nil {
		log.Fatal(err)
	}

	if err := db.Model(&srv1).Association("Roles").Append(role1).Error; err != nil {
		log.Fatal(err)
	}

	if err := db.Model(&usr1).Association("Roles").Append(role1).Error; err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		text, _ := now.MarshalText()
		w.Write(text)
	})
	http.ListenAndServe(":10000", mux)

}

func getUserToken() string {
	provider, err := oidc.NewProvider(context.Background(), "http://localhost:8080/auth/realms/master")
	if err != nil {
		// handle error
	}

	// Configure an OpenID Connect aware OAuth2 client.
	oauth2Config := oauth2.Config{
		ClientID:     "safescale",
		ClientSecret: "safescale",
		RedirectURL:  "",

		// Discovery returns the OAuth2 endpoints.
		Endpoint: provider.Endpoint(),

		// "openid" is a required scope for OpenID Connect flows.
		Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
	}
	token, err := oauth2Config.PasswordCredentialsToken(context.Background(), "user", "user")
	if err != nil {
		return ""
	}
	return token.AccessToken

}

func TestGateway(t *testing.T) {
	Clean()

	go gateway.Start(":4443")
	go runTestService()
	time.Sleep(2 * time.Second)
	resp, err := http.Get("http://localhost:10000/date")
	assert.Nil(t, err)
	text, err := ioutil.ReadAll(resp.Body)
	assert.Nil(t, err)
	fmt.Println(string(text))

	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	token := getUserToken()
	req, err := http.NewRequest("GET", "https://localhost:4443/TEST", nil)
	assert.Nil(t, err)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	client := http.Client{}
	resp, err = client.Do(req)
	assert.Nil(t, err)
	text, err = ioutil.ReadAll(resp.Body)
	assert.Nil(t, err)
	fmt.Println(string(text))
	for k, v := range resp.Header {
		fmt.Println(k, v)
	}

}
