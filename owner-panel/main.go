package main

import (
 "crypto/hmac"
 "crypto/rand"
 "crypto/sha256"
 "database/sql"
 "encoding/base64"
 "encoding/json"
 "io"
 "fmt"
 "log"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "strconv"
 "time"

 _ "modernc.org/sqlite"
)
type Server struct{ db *sql.DB; ownerKey string; secret []byte }
type User struct{ ID string `json:"id"`; Label string `json:"label"`; Access string `json:"access"`; First string `json:"first_seen"`; Last string `json:"last_seen"` }
type Release struct{ ID int64 `json:"id"`; Name string `json:"name"`; URL string `json:"url"`; Notes string `json:"notes"`; Created string `json:"created_at"` }
func main(){
 key:=os.Getenv("OWNER_KEY");if len(key)<20{log.Fatal("Set OWNER_KEY to a random secret of at least 20 characters")}
 path:=os.Getenv("DATABASE_PATH");if path==""{path="owner-panel-data.sqlite"};if err:=os.MkdirAll(filepath.Dir(path),0755);err!=nil{log.Fatal(err)}
 db,err:=sql.Open("sqlite",path);if err!=nil{log.Fatal(err)};defer db.Close()
 for _,q:=range []string{
 `CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY,label TEXT NOT NULL DEFAULT '',access TEXT NOT NULL DEFAULT 'member',first_seen TEXT NOT NULL,last_seen TEXT NOT NULL)`,
 `CREATE TABLE IF NOT EXISTS releases(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,url TEXT NOT NULL,notes TEXT NOT NULL,created_at TEXT NOT NULL)`,
 }{if _,err=db.Exec(q);err!=nil{log.Fatal(err)}}
 secret:=make([]byte,32);if _,err=rand.Read(secret);err!=nil{log.Fatal(err)}
 s:=&Server{db:db,ownerKey:key,secret:secret};mux:=http.NewServeMux()
 mux.HandleFunc("/api/owner/login",s.login);mux.HandleFunc("/api/owner/users",s.auth(s.users));mux.HandleFunc("/api/owner/users/",s.auth(s.access));mux.HandleFunc("/api/owner/releases",s.auth(s.releases));mux.HandleFunc("/api/app/check-in",s.checkin)
 mux.Handle("/",http.FileServer(http.Dir(".")))
 addr:=":"+env("PORT","8080");log.Printf("LunarVoid Owner Panel listening on %s",addr);log.Fatal(http.ListenAndServe(addr,securityHeaders(mux)))
}
func env(k,d string)string{if v:=os.Getenv(k);v!=""{return v};return d}
func securityHeaders(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Referrer-Policy","no-referrer");w.Header().Set("Cache-Control","no-store");if r.Method==http.MethodOptions{w.Header().Set("Access-Control-Allow-Origin","*");w.Header().Set("Access-Control-Allow-Headers","Authorization, Content-Type");w.Header().Set("Access-Control-Allow-Methods","GET,POST,PUT,OPTIONS");w.WriteHeader(204);return};next.ServeHTTP(w,r)})}
func jsonOut(w http.ResponseWriter,status int,v any){w.Header().Set("Content-Type","application/json");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(v)}
func body(r *http.Request,v any)error{return json.NewDecoder(io.LimitReader(r.Body,1<<20)).Decode(v)}
func(s *Server)login(w http.ResponseWriter,r *http.Request){if r.Method!="POST"{jsonOut(w,405,map[string]string{"error":"Method not allowed"});return};var in struct{Key string `json:"key"`};if body(r,&in)!=nil||!hmac.Equal([]byte(in.Key),[]byte(s.ownerKey)){jsonOut(w,401,map[string]string{"error":"Invalid owner key"});return};expires:=time.Now().Add(12*time.Hour).Unix();payload:=base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("owner:%d",expires)));mac:=hmac.New(sha256.New,s.secret);mac.Write([]byte(payload));sig:=base64.RawURLEncoding.EncodeToString(mac.Sum(nil));jsonOut(w,200,map[string]string{"token":payload+"."+sig})}
func(s *Server)auth(next http.HandlerFunc)http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){p:=strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer "),".");if len(p)!=2{jsonOut(w,401,map[string]string{"error":"Unauthorized"});return};mac:=hmac.New(sha256.New,s.secret);mac.Write([]byte(p[0]));sig,err:=base64.RawURLEncoding.DecodeString(p[1]);if err!=nil||!hmac.Equal(sig,mac.Sum(nil)){jsonOut(w,401,map[string]string{"error":"Unauthorized"});return};raw,err:=base64.RawURLEncoding.DecodeString(p[0]);if err!=nil{jsonOut(w,401,map[string]string{"error":"Unauthorized"});return};parts:=strings.SplitN(string(raw),":",2);if len(parts)!=2||parts[0]!="owner"{jsonOut(w,401,map[string]string{"error":"Session expired"});return};exp,err:=strconv.ParseInt(parts[1],10,64);if err!=nil||time.Now().Unix()>exp{jsonOut(w,401,map[string]string{"error":"Session expired"});return};next(w,r)}}
func(s *Server)checkin(w http.ResponseWriter,r *http.Request){if r.Method!="POST"{jsonOut(w,405,map[string]string{"error":"Method not allowed"});return};var in struct{ID string `json:"install_id"`;Label string `json:"label"`};if body(r,&in)!=nil||len(in.ID)<16||len(in.ID)>100{jsonOut(w,400,map[string]string{"error":"Valid install_id required"});return};now:=time.Now().UTC().Format(time.RFC3339);label:=strings.TrimSpace(in.Label);if len(label)>64{label=label[:64]};_,err:=s.db.Exec(`INSERT INTO users(id,label,first_seen,last_seen) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET label=CASE WHEN excluded.label<>'' THEN excluded.label ELSE users.label END,last_seen=excluded.last_seen`,in.ID,label,now,now);if err!=nil{jsonOut(w,500,map[string]string{"error":"Could not record check-in"});return};jsonOut(w,200,map[string]bool{"ok":true})}
func(s *Server)users(w http.ResponseWriter,r *http.Request){if r.Method!="GET"{jsonOut(w,405,map[string]string{"error":"Method not allowed"});return};rows,err:=s.db.Query(`SELECT id,label,access,first_seen,last_seen FROM users ORDER BY last_seen DESC`);if err!=nil{jsonOut(w,500,map[string]string{"error":"Database error"});return};defer rows.Close();list:=[]User{};for rows.Next(){var u User;if rows.Scan(&u.ID,&u.Label,&u.Access,&u.First,&u.Last)==nil{if u.Label==""{u.Label="Install "+u.ID[:8]};list=append(list,u)}};jsonOut(w,200,map[string]any{"users":list})}
func(s *Server)access(w http.ResponseWriter,r *http.Request){if r.Method!="PUT"{jsonOut(w,405,map[string]string{"error":"Method not allowed"});return};id:=strings.TrimPrefix(r.URL.Path,"/api/owner/users/");id=strings.TrimSuffix(id,"/access");if id==""||strings.Contains(id,"/"){jsonOut(w,400,map[string]string{"error":"Invalid user"});return};var in struct{Access string `json:"access"`};if body(r,&in)!=nil||(in.Access!="owner"&&in.Access!="beta_tester"&&in.Access!="member"){jsonOut(w,400,map[string]string{"error":"Access must be owner, beta_tester or member"});return};res,err:=s.db.Exec("UPDATE users SET access=? WHERE id=?",in.Access,id);if err!=nil{jsonOut(w,500,map[string]string{"error":"Database error"});return};n,_:=res.RowsAffected();if n==0{jsonOut(w,404,map[string]string{"error":"User not found"});return};jsonOut(w,200,map[string]bool{"ok":true})}
func(s *Server)releases(w http.ResponseWriter,r *http.Request){switch r.Method{case "GET":rows,err:=s.db.Query("SELECT id,name,url,notes,created_at FROM releases ORDER BY id DESC");if err!=nil{jsonOut(w,500,map[string]string{"error":"Database error"});return};defer rows.Close();out:=[]Release{};for rows.Next(){var x Release;if rows.Scan(&x.ID,&x.Name,&x.URL,&x.Notes,&x.Created)==nil{out=append(out,x)}};jsonOut(w,200,map[string]any{"releases":out})
case "POST":var x Release;if body(r,&x)!=nil||strings.TrimSpace(x.Name)==""||!strings.HasPrefix(x.URL,"https://"){jsonOut(w,400,map[string]string{"error":"Name and HTTPS download URL required"});return};if len(x.Name)>100||len(x.Notes)>2000{jsonOut(w,400,map[string]string{"error":"Release details too long"});return};x.Created=time.Now().UTC().Format(time.RFC3339);res,err:=s.db.Exec("INSERT INTO releases(name,url,notes,created_at) VALUES(?,?,?,?)",x.Name,x.URL,x.Notes,x.Created);if err!=nil{jsonOut(w,500,map[string]string{"error":"Could not save release"});return};x.ID,_=res.LastInsertId();jsonOut(w,201,x)
default:jsonOut(w,405,map[string]string{"error":"Method not allowed"})}}
