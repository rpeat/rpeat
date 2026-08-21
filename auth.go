package rpeat

// https://medium.com/technology-learning/how-we-solved-authentication-and-authorization-in-our-microservice-architecture-994539d1b6e6
import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"github.com/go-kit/kit/endpoint"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"strings"
)

type TLS struct {
	Cert string `json:"Cert,omitempty" xml:"Cert,omitempty"`
	Key  string `json:"Key,omitempty" xml:"Key,omitempty"`
}

// https://stackoverflow.com/questions/21936332/idiomatic-way-of-requiring-http-basic-auth-in-go
func checkUserMiddleware(user string) endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, request interface{}) (interface{}, error) {
			return next(ctx, request)
		}
	}
}

func CurrentUser(r *http.Request) string {

	user, _, ok := r.BasicAuth()
	if !ok {
		userCookie, uerr := r.Cookie("KRON_USER")
		if uerr == nil {
			user = userCookie.Value
		}
	}

	return user
}

func getBasicAuthOrCookie(r *http.Request) (string, string, bool) {
	var user, pass string
	var ok bool
	if user, pass, ok = r.BasicAuth(); !ok {
		xAuth, err := r.Cookie("X-RPEAT-Authorization")
		if err != nil {
			return user, pass, ok
		}
		authString, err := base64.StdEncoding.DecodeString(xAuth.Value)
		if err != nil {
			return user, pass, ok
		}
		user_pw := strings.Split(string(authString), ":")
		if len(user_pw) == 2 {
			user = user_pw[0]
			pass = user_pw[1]
		}
	}
	return user, pass, ok
}

type contextKey int

const authenticatedUserKey contextKey = 0

func authenticateUser(handler http.Handler, users []AuthUser, id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		realm := "rpeat"
		var user, pass string
		var ok bool
		authenticated := false

		//ConnectionLogger.Printf("%s request for %s (%s) from %s", r.Method, id, r.URL, r.RemoteAddr)

		// https://stackoverflow.com/questions/4361173/http-headers-in-websockets-client-api
		accessCookie, accesserr := r.Cookie("RPEAT_ACCESS")
		refreshCookie, _ := r.Cookie("RPEAT_REFRESH")

		if accesserr != nil {
			//user, pass, ok = r.BasicAuth()  // if xAuth is passed (we are in /api/updates && using BasicAuth), user,pass,ok := XRPEATAuth(xAuth)
			user, pass, ok = getBasicAuthOrCookie(r)
			http.SetCookie(w, &http.Cookie{Name: "X-RPEAT-Authorization", Value: strings.Replace(r.Header.Get("Authorization"), "Basic ", "", 1), SameSite: http.SameSiteStrictMode})
			for _, u := range users {
				if subtle.ConstantTimeCompare([]byte(user), []byte(u.User)) == 1 &&
					subtle.ConstantTimeCompare([]byte(pass), []byte(u.Secret)) == 1 {
					authenticated = true
					break
				}
			}
			if !authenticated {
				if r.URL.String() != "/api/updates" {
					w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
					w.WriteHeader(401)
					w.Write([]byte("Unauthorised.\n"))
					return
				} else {
					w.WriteHeader(401)
					w.Write([]byte("Unauthorised.\n"))
					return
				}
			}
		} else {
			ConnectionLogger.Printf("[rpeat.AuthenticateUser] using rpeat.io authentication via Authorize")
			if accesserr == nil {
				accessToken := accessCookie.Value
				refreshToken := refreshCookie.Value
				ConnectionLogger.Printf("requesting authentication and authortization for id:%s from rpeat.io", id)
				ConnectionLogger.Printf("[rpeat.AuthenticateUser] requesting authentication and authortization for id:%s from rpeat.io", id)
				var updatedAccessToken string
				updatedAccessToken, user, ok = Authorize(accessToken, refreshToken, id)
				if ok {
					ServerLogger.Printf("[rpeat.AuthenticateUser] authentication success. (updated?) accesssToken: %s", updatedAccessToken[len(updatedAccessToken)-10:])
					authenticated = true
					ctxWithUser := context.WithValue(r.Context(), authenticatedUserKey, user)
					rWithUser := r.WithContext(ctxWithUser)
					accessCookie.Value = updatedAccessToken
					accessCookie.Domain = ".rpeat.io"
					accessCookie.Path = "/"
					accessCookie.Expires = time.Now().Add(time.Second * 3600)
					http.SetCookie(w, accessCookie)
					handler.ServeHTTP(w, rWithUser)
					return
				} else {
					ServerLogger.Println("[rpeat.AuthenticateUser] authentication failed") // FIXME need to send JSON response back to user to prompt login
					return
				}
			}
		}

		handler.ServeHTTP(w, r)
	})
}

var AuthenticateUser = authenticateUser

func GetUserFromAuth(r *http.Request) (string, bool) {
	var ok bool
	var user string
	//user, _, ok = r.BasicAuth()
	user, _, ok = getBasicAuthOrCookie(r)

	if !ok { // using api
		value := r.Context().Value(authenticatedUserKey)
		if value != nil {
			user = value.(string)
			ok = true
		}
	}
	return user, ok
}

// A map for each API action, e.g. {"hold":["gwashington","jkennedy","dtrump"],"start":["gwashington"]}
type Permission map[string][]string
type Permission2 struct {
	Hold    []string
	Start   []string
	Stop    []string
	Restart []string
	Info    []string
}

func (job *Job) hasPermission(user, action string) bool {
	if job.User == user || stringInSlice(user, job.Admin) {
		// job.User and job.Admin have full access
		return true
	} else {
		// all additional access is controlled through user permissions
		// this would be nice to model as deny/allow
		var authorized, any []string

		authorized, _ = job.Permissions[action]
		any, _ = job.Permissions["all"]

		authorized = append(authorized, any...)
		for _, a := range authorized {
			if a == user || a == "*" {
				return true
			}
		}
	}
	return false
}
func (job *Job) HasPermission(user, action string) bool {
	return job.hasPermission(user, action)
}

//	func (job *Job) HasPermission(user, action string) bool {
//	   if job.User == user {
//	       // job.User has full access always
//	       return true
//	   }
//	   if stringInSlice(user, job.Admin) {
//	       var authorized, any []string
//
//	       authorized, _ = job.Permissions[action]
//	       any, _ = job.Permissions["all"]
//
//	       authorized = append(authorized, any...)
//	       for _, a := range authorized {
//	           if a == user || a == "*" {
//	               return true
//	           }
//	       }
//	   }
//	   return false
//	}
func (s ServerConfig) hasPermission(user, action string) bool {
	if s.Owner == user {
		return true
	}
	if stringInSlice(user, s.Admin) {
		var authorized, any []string

		authorized, _ = s.Permissions[action]
		any, _ = s.Permissions["all"]

		authorized = append(authorized, any...)
		for _, a := range authorized {
			if a == user || a == "*" {
				return true
			}
		}
	}
	return false
}

type AuthUser struct {
	User   string
	Groups []string
	Secret string
}

func LoadAuth(auth string) ([]AuthUser, error) {
	authFile, err := os.Open(auth)

	byteval, _ := ioutil.ReadAll(authFile)
	authFile.Close()

	var users []AuthUser

	err = json.Unmarshal(byteval, &users)

	return users, err
}

func Authorize(access, refresh, id string) (string, string, bool) {

	// Security is not an issue here as rpeat.io certificate is trusted FIXME: remove
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	URL := "https://api.rpeat.io/authorize"
	type User struct {
		Access string
		User   string
		Ok     bool
	}
	ConnectionLogger.Printf("[Authorize] id: %s (%s)", id, URL)
	ConnectionLogger.Printf("[Authorize] using accessToken: %s", access[len(access)-10:])
	ConnectionLogger.Printf("[Authorize] using refreshToken: %s", refresh[len(refresh)-10:])
	data, _ := json.Marshal(map[string]string{"Access": access, "Refresh": refresh, "ID": id})
	req, err := http.NewRequest("POST", URL, bytes.NewBuffer(data))
	resp, err := client.Do(req)
	if err != nil {
		ConnectionLogger.Println("authentication|authorization error:", err)
	}
	defer resp.Body.Close()

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		ConnectionLogger.Println("authentication unmarshalling error:", err)
	}

	ConnectionLogger.Printf("[Authorize]: Access: %s User:%s Ok:%t", user.Access[len(user.Access)-10:], user.User, user.Ok)
	return user.Access, user.User, user.Ok
}
