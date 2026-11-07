package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticDashboardAndSameOriginAPIProxy(t *testing.T){
	root:=t.TempDir()
	if err:=os.WriteFile(filepath.Join(root,"index.html"),[]byte("<h1>Mail Sender</h1>"),0600);err!=nil{t.Fatal(err)}
	backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.URL.Path!="/v1/session"||r.Host!="dashboard.example.com"||r.Header.Get("Authorization")!="Bearer sentinel"{t.Error("proxy changed path, host, or authorization")}
		http.SetCookie(w,&http.Cookie{Name:"sender_session",Value:"session-value",HttpOnly:true,Path:"/"})
		w.Header().Set("Content-Type","application/json")
		_,_=io.WriteString(w,"{\"data\":{\"ok\":true}}")
	}));defer backend.Close()
	handler,err:=handler(root,backend.URL);if err!=nil{t.Fatal(err)}
	r:=httptest.NewRequest("GET","http://dashboard.example.com/",nil)
	w:=httptest.NewRecorder();handler.ServeHTTP(w,r)
	if w.Code!=200||!strings.Contains(w.Body.String(),"Mail Sender"){t.Fatal("static dashboard missing")}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"),"frame-ancestors 'none'"){t.Fatal("dashboard security policy missing")}
	r=httptest.NewRequest("POST","http://dashboard.example.com/api/v1/session",strings.NewReader("{}"));r.Header.Set("Authorization","Bearer sentinel")
	w=httptest.NewRecorder();handler.ServeHTTP(w,r)
	if w.Code!=200||len(w.Result().Cookies())!=1||!w.Result().Cookies()[0].HttpOnly{t.Fatal("proxy did not preserve the session cookie")}
}

func TestAPIOriginRejectsCredentialsAndPaths(t *testing.T){
	for _,value:=range []string{"file:///tmp","https://user:password@example.com","https://example.com/private","https://example.com?token=x"}{
		if _,err:=handler(t.TempDir(),value);err==nil{t.Fatalf("accepted unsafe API origin: %s",value)}
	}
}
