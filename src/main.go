package main

import (
    "container/list"
    "container/ring"
    "fmt"
    "http"
    "json"
    "os"
    "rand"
    "regexp"
    "runtime"
    "strconv"
    "time"
)

var (
    room *Room
    rollOffRoute *regexp.Regexp
)

type User struct {
    Username string
    c chan *ChatMessage
    quit chan bool
}

func NewUser(username string) *User {
    u := &User{Username: username}
    u.c = make(chan *ChatMessage, 20)
    return u
}

type ChatMessage struct {
    MsgType string
    Username string
    Body string
    TimeStamp *time.Time
}

func (m *ChatMessage)WriteToResponse(w http.ResponseWriter) {
    w.Header()["Content-Type"] = []string{"application/json"}
    raw, err := json.Marshal([]*ChatMessage{m})
    if err != nil {
        fmt.Fprintf(os.Stderr, "something got fucked up in json.Marshal.\n")
    } else {
        w.Write(raw)
    }
}

type Room struct {
    Users *list.List
    Messages *ring.Ring
    c chan *ChatMessage
}

func NewRoom() *Room {
    r := new(Room)
    r.Users = list.New()
    r.Messages = ring.New(20)
    return r
}

func (r *Room)getUserElement(username string) (*list.Element, *User) {
    for e := r.Users.Front(); e != nil; e = e.Next() {
        user := e.Value.(*User)
        if user.Username == username {
            return e, user
        }
    }
    return nil, nil
}

func (r *Room)GetUser(username string) *User {
    _, user := r.getUserElement(username)
    return user
}

func (r *Room)GetAllUsers() []*User {
    users := make([]*User, r.Users.Len())
    i := 0
    for e := r.Users.Front(); e != nil; e = e.Next() {
        users[i] = e.Value.(*User)
        i += 1
    }
    return users
}

func (r *Room)AddUser(username string) (*User, os.Error) {
    user := r.GetUser(username)
    if user != nil {
        return nil, os.NewError("That username is already taken.")
    }
    user = NewUser(username)
    r.Users.PushBack(user)
    r.Announce(fmt.Sprintf("%s has entered the room.", username), false)
    <-user.c
    return user, nil
}

func (r *Room)RemoveUser(username string) bool {
    if e, _ := r.getUserElement(username); e != nil {
        r.Users.Remove(e)
        r.Announce(fmt.Sprintf("%s has left the room.", username), false)
        return true
    }
    return false
}

func (r *Room)AddMessage(msg *ChatMessage) {
    r.Messages = r.Messages.Next()
    r.Messages.Value = msg
    for elem := r.Users.Front(); elem != nil; elem = elem.Next() {
        go func(e *list.Element, m *ChatMessage) {
            user := e.Value.(*User)
            user.c <- m
        }(elem, msg)
    }
}

func (r *Room)MessageHistory() []*ChatMessage {
    messages := make([]*ChatMessage, r.Messages.Len())
    i := 0
    r.Messages.Do(func(val interface{}) {
        if val == nil { return }
        messages[i] = val.(*ChatMessage)
        i += 1
    })
    return messages[0:i]
}

type RollOff struct {
    Id string
    Entries []*RollOffEntry
}

type RollOffEntry struct {
    User *User
    Score int
}

func (r *Room)Announce(msgText string, isError bool) {
    msg := &ChatMessage{
        Body: msgText,
        TimeStamp: time.UTC(),
    }

    if isError { msg.MsgType = "error" } else { msg.MsgType = "system" }
    r.AddMessage(msg)
}

func ParseJSONField(r *http.Request, fieldname string) (string, os.Error) {
    requestLength, err := strconv.Atoui(r.Header["Content-Length"][0])
    if err != nil {
        fmt.Fprintf(os.Stderr, "unable to convert incoming login request content-lenth to uint.")
    }
    var l map [string] string
    raw := make([]byte, requestLength)
    r.Body.Read(raw)
    if err := json.Unmarshal(raw, &l); err != nil {
        return "", err
    }
    return l[fieldname], nil
}

func JSONResponse(w http.ResponseWriter, val interface{}) {
    w.Header()["Content-Type"] = []string{"application/json"}
    raw, err := json.Marshal(val)
    if err != nil {
        fmt.Fprintf(os.Stderr, "something got fucked up in json.Marshal.\n")
    } else {
        w.Write(raw)
    }
}

// given an http.Request r, returns the username associated with the given
// request, as determined with an extremely unsafe cookie.  Returns an empty
// string if the user is not logged in.
func ParseUsername(r *http.Request) string {
    for _, c := range r.Cookies() {
        if c.Name == "username" {
            return c.Value
        }
    }
    return ""
}

func ParseMessage(r *http.Request) (*ChatMessage, os.Error) {
    msgLength, err := strconv.Atoui(r.Header["Content-Length"][0])
    if err != nil {
        fmt.Fprintf(os.Stderr, "unable to convert incoming message content-length to uint.")
    }
    from := room.GetUser(ParseUsername(r))

    m := &ChatMessage{Username: from.Username, TimeStamp: time.UTC(), MsgType: "user"}
    raw := make([]byte, msgLength)
    r.Body.Read(raw)
    if err := json.Unmarshal(raw, m); err != nil {
        fmt.Fprintf(os.Stderr, "%s\n", err)
    }
    return m, err
}

func Home(w http.ResponseWriter, r *http.Request) {
    if r.RawURL == "/favicon.ico" { return }
    http.ServeFile(w, r, "templates/index.html")
}

func LoginMux(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case "POST": Login(w, r)
    case "DELETE": Logout(w, r)
    }
}

func LogWrap(fn func(http.ResponseWriter, *http.Request), label string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.RawURL != "/favicon.ico" {
            fmt.Fprintf(os.Stdout, "%s %s\n", r.Method, r.RawURL)
        }
        fmt.Printf("Handler: %s\n", label)
        fn(w, r)
    }
}

func Login(w http.ResponseWriter, r *http.Request) {
    username, err := ParseJSONField(r, "username")
    if err != nil {
        http.Error(w, err.String(), http.StatusInternalServerError)
        return
    }

    user, err := room.AddUser(username)
    if err != nil {
        http.Error(w, err.String(), http.StatusInternalServerError)
        return
    }

    cookie := &http.Cookie{Name: "username", Value: user.Username, HttpOnly: true}
    http.SetCookie(w, cookie)
    JSONResponse(w, room.MessageHistory())
}

func Logout(w http.ResponseWriter, r *http.Request) {
    username := ParseUsername(r)
    if username == "" {
        http.Error(w, "That username wasn't actually logged in.", http.StatusInternalServerError)
    }
    room.RemoveUser(username)
}

func FeedMux(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case "GET": Poll(w, r)
    case "POST": Post(w, r)
    }
}

func Post(w http.ResponseWriter, r *http.Request) {
    m, err := ParseMessage(r)
    if err != nil {
        http.Error(w, "Unable to parse incoming chat message", http.StatusInternalServerError)
    }
    room.AddMessage(m)
    fmt.Fprintf(os.Stdout, "\t%s: %s\n", m.Username, m.Body)
}

func Poll(w http.ResponseWriter, r *http.Request) {
    user := room.GetUser(ParseUsername(r))
    timeout := make(chan bool, 1)
    go func() { time.Sleep(1.2e11); timeout <- true }() // two minute timeout
    if user != nil && user.c != nil {
        select {
        case msg := <-user.c: msg.WriteToResponse(w)
        case <-timeout: return
        }
    } else {
        fmt.Fprintf(os.Stderr, "the user %s has a null incoming channel.\n", user.Username)
        return
    }
}

func GetUsers(w http.ResponseWriter, r *http.Request) {
    JSONResponse(w, room.GetAllUsers())
}

func Roll(w http.ResponseWriter, r *http.Request) {
    num := rand.Intn(100) + 1
    username := ParseUsername(r)
    room.Announce(fmt.Sprintf("%s rolled %d\n", username, num), false)
}

func RollOffMux(w http.ResponseWriter, r *http.Request) {
    route := regexp.MustCompile("^/roll-off/(.*)$")
    matches := route.FindStringSubmatch(r.RawURL)
    fmt.Printf("%s matching %s? %#v\n", r.RawURL, route.String(), matches)
    id := matches[1]
    if id == "" {
        NewRollOff(w, r)
    } else {
        EnterRollOff(w, r)
    }
}

func NewRollOff(w http.ResponseWriter, r *http.Request) {
    username := ParseUsername(r)
    fmt.Printf("%s starting a roll-off\n", username)

    entry := new(RollOffEntry)
    entry.Score = rand.Intn(100) + 1
    entry.User = room.GetUser(username)

    rolloff := new(RollOff)
    rolloff.Id = randomString(20)
    rolloff.Entries = []*RollOffEntry{entry}

    linkId := fmt.Sprintf("%s-link", rolloff.Id)
    spanId := fmt.Sprintf("%s-span", rolloff.Id)
    appendTag := fmt.Sprintf(`<br /><a href='javascript:void(0);' id='%s'>click here to join the roll-off.</a>`, linkId)

    script := fmt.Sprintf(`<span id="%s">%s has started a roll off!<br />%s rolled %d</span>
    <script>
      console.log("This is injected.  My username: ", Chat.getUsername());
      if(Chat.getUsername() != "%s") {
        console.log("Injected link.");
        $("#%s").append("%s");
        $("#%s").click(function(event){
          $.ajax({
            type: "POST",
            url: "/roll-off-entry/%s",
            async: true,
            timeout: 60000,
            success: function(data) {
              $(this).remove();
            },
            error: function(XMLHttpRequest, textStatus, errorThrown) {
              console.log(XMLHttpRequest, textStatus, errorThrown);
            }
          });
        });
      } else {
        console.log("Did not inject link.");
      }
    </script>`, spanId, username, username, entry.Score, username, spanId, appendTag, linkId, rolloff.Id)
    fmt.Println(script)
    room.Announce(script, false)
}

func EnterRollOff(w http.ResponseWriter, r *http.Request) {
    //linkId := fmt.Sprintf("%s-link", id)
}

func main() {
    runtime.GOMAXPROCS(8)
    port := "0.0.0.0:8080"
    room = NewRoom()
    staticDir := http.Dir("/projects/go/chat/static")
    staticServer := http.FileServer(staticDir)

    http.HandleFunc("/", LogWrap(Home, "Home"))
    http.HandleFunc("/feed", LogWrap(FeedMux, "FeedMux"))
    http.HandleFunc("/login", LogWrap(LoginMux, "LoginMux"))
    http.HandleFunc("/users", LogWrap(GetUsers, "GetUsers"))
    http.HandleFunc("/roll", LogWrap(Roll, "Roll"))
    http.HandleFunc("/roll-off", LogWrap(NewRollOff, "NewRollOff"))
    http.HandleFunc("/roll-off-entry/", LogWrap(EnterRollOff, "EnterRollOff"))
    http.Handle("/static/", http.StripPrefix("/static", staticServer))
    fmt.Printf("Serving at %s ----------------------------------------------------\n", port)
    http.ListenAndServe(port, nil)
}

/*------------------------------------------------------------------------------
*
* Odds and ends
*
*-----------------------------------------------------------------------------*/

func randomString(length int) string {
    raw := make([]int, length)
    for i := range raw {
        raw[i] = 97 + rand.Intn(26)
    }
    return string(raw)
}

