// Package google provides you access to Google's OAuth2
// infrastructure. The implementation is based on this blog post:
// http://skarlso.github.io/2016/06/12/google-signin-with-go/
package google

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
	goauth "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Credentials stores google client-ids.
type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

const (
	stateKey  = "state"
	sessionID = "ginoauth_google_session"
)

var (
	conf  *oauth2.Config
	store sessions.Store
)

func init() {
	gob.Register(goauth.Userinfo{})
}

func randToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		glog.Fatalf("[Gin-OAuth] Failed to read rand: %v", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// Setup the authorization path
func Setup(redirectURL, credFile string, scopes []string, secret []byte) {
	store = cookie.NewStore(secret)

	var c Credentials
	file, err := ioutil.ReadFile(credFile)
	if err != nil {
		glog.Fatalf("[Gin-OAuth] File error: %v", err)
	}
	if err := json.Unmarshal(file, &c); err != nil {
		glog.Fatalf("[Gin-OAuth] Failed to unmarshal client credentials: %v", err)
	}

	conf = &oauth2.Config{
		ClientID:     "19155837812-t51045bpfce2c640f097ar7v6rlkj6ed.apps.googleusercontent.com",
		ClientSecret: "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",
		RedirectURL:  redirectURL,
		Scopes:       scopes,
		Endpoint:     google.Endpoint,
	}
}

func Session(name string) gin.HandlerFunc {
	return sessions.Sessions(name, store)
}

func LoginHandler(ctx *gin.Context) {
	stateValue := randToken()
	session := sessions.Default(ctx)
	session.Set(stateKey, stateValue)
	session.Save()
	ctx.Writer.Write([]byte(`
	<html>
		<head>
			<title>Golang Google</title>
		</head>
	  <body>
			<a href='` + GetLoginURL(stateValue) + `'>
				<button>Login with Google!</button>
			</a>
		</body>
	</html>`))
}

func GetLoginURL(state string) string {
	return conf.AuthCodeURL(state)
}

// Auth is the google authorization middleware. You can use them to protect a routergroup.
// Example:
//
//        private.Use(google.Auth())
//        private.GET("/", UserInfoHandler)
//        private.GET("/api", func(ctx *gin.Context) {
//            ctx.JSON(200, gin.H{"message": "Hello from private for groups"})
//        })
//
//    // Requires google oauth pkg to be imported as `goauth "google.golang.org/api/oauth2/v2"`
//    func UserInfoHandler(ctx *gin.Context) {
// 	      var (
// 	      	res goauth.Userinfo
// 	      	ok  bool
// 	      )
//
// 	      val := ctx.MustGet("user")
// 	      if res, ok = val.(goauth.Userinfo); !ok {
// 	      	res = goauth.Userinfo{Name: "no user"}
// 	      }
//
// 	      ctx.JSON(http.StatusOK, gin.H{"Hello": "from private", "user": res.Email})
//    }
func Auth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// Handle the exchange code to initiate a transport.
		session := sessions.Default(ctx)

		existingSession := session.Get(sessionID)
		if userInfo, ok := existingSession.(goauth.Userinfo); ok {
			ctx.Set("user", userInfo)
			ctx.Next()
			return
		}

		retrievedState := session.Get(stateKey)
		if retrievedState != ctx.Query(stateKey) {
			ctx.AbortWithError(http.StatusUnauthorized, fmt.Errorf("invalid session state: %s", retrievedState))
			return
		}

		tok, err := conf.Exchange(context.TODO(), ctx.Query("code"))
		if err != nil {
			ctx.AbortWithError(http.StatusBadRequest, fmt.Errorf("failed to exchange code for oauth token: %w", err))
			return
		}

		oAuth2Service, err := goauth.NewService(ctx, option.WithTokenSource(conf.TokenSource(ctx, tok)))
		if err != nil {
			glog.Errorf("[Gin-OAuth] Failed to create oauth service: %v", err)
			ctx.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to create oauth service: %w", err))
			return
		}

		userInfo, err := oAuth2Service.Userinfo.Get().Do()
		if err != nil {
			glog.Errorf("[Gin-OAuth] Failed to get userinfo for user: %v", err)
			ctx.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get userinfo for user: %w", err))
			return
		}

		ctx.Set("user", userInfo)

		session.Set(sessionID, userInfo)
		if err := session.Save(); err != nil {
			glog.Errorf("[Gin-OAuth] Failed to save session: %v", err)
			ctx.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to save session: %v", err))
			return
		}
	}
}
