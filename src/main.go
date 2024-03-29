package main

import (
    "bytes"
    "container/list"
    "container/ring"
    "fmt"
    "github.com/russross/blackfriday"
    "http"
    "io/ioutil"
    "json"
    "os"
    "rand"
    "regexp"
    "runtime"
    "strconv"
    "template"
    "time"
)

var (
    room *Room
    rolloffs []*RollOff
    rollOffRoute *regexp.Regexp
    homeTemplate *template.Template
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
    Id int
    MsgType string
    Username string
    Body string
    Timestamp *time.Time
}

var NextId = func() func() int {
    current := 0
    return func() int {
        current += 1
        return current
    }
}()

func NewMessage(username string, body string, msgtype string) *ChatMessage {
    return &ChatMessage{Username: username, Body: body, MsgType: msgtype,
                        Timestamp: time.UTC(), Id: NextId()}
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

var urlPattern *regexp.Regexp = regexp.MustCompile("https?://[^ \t\n]+")
func (m *ChatMessage)Links() {
    fmt.Println(m.Body)
    m.Body = string(Render([]byte(m.Body)))
    matches := urlPattern.FindAllStringIndex(m.Body, -1)
    for _, match := range(matches) {
        start, end := match[0], match[1]
        leader := `href="`
        if start > len(leader) && m.Body[start-len(leader):start] == leader {
            continue
        }
        url := m.Body[start:end]
        embed, found := GetEmbed(url)
        if found {
            m.Body = m.Body[:start] + embed + m.Body[end:]
        }
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
    msg.Links()
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
    Open bool
}

type RollOffEntry struct {
    User *User
    Score int
    RollOff *RollOff
}

func (r *RollOff)AddEntry(e *RollOffEntry) {
    r.Entries = append(r.Entries, e)
    e.RollOff = r
}

func (r *Room)Announce(msgText string, isError bool) {
    var msgType string
    if isError { msgType = "error" } else { msgType = "system" }
    msg := NewMessage("system", msgText, msgType)
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

func ParseUser(r *http.Request) *User {
    return room.GetUser(ParseUsername(r))
}

func ParseMessage(r *http.Request) (*ChatMessage, os.Error) {
    msgLength, err := strconv.Atoui(r.Header["Content-Length"][0])
    if err != nil {
        fmt.Fprintf(os.Stderr, "unable to convert incoming message content-length to uint.")
    }
    from := ParseUser(r)

    m := &ChatMessage{Username: from.Username, Timestamp: time.UTC(), MsgType: "user", Id: NextId()}
    raw := make([]byte, msgLength)
    r.Body.Read(raw)
    if err := json.Unmarshal(raw, m); err != nil {
        fmt.Fprintf(os.Stderr, "%s\n", err)
    }
    return m, err
}

func Home(w http.ResponseWriter, r *http.Request) {
    /*
    username := ParseUsername(r)
    if username != "" {
    }
    */
    if r.RawURL == "/favicon.ico" { return }
    // http.ServeFile(w, r, "templates/index.html")
    homeTemplate.Execute(w, new(interface{}))
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
    go room.AddMessage(m)
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

func TellUserElement(e *list.Element, m *ChatMessage) {
    user := e.Value.(*User)
    user.c <- m
}

func (m *ChatMessage)Write(p []byte) (n int, err os.Error) {
    m.Body += string(p)
    return len(p), nil
}

func (r *RollOff)Cycle() {
    time.Sleep(3e10)
    r.Open = false
    var winningEntry *RollOffEntry
    for _, entry := range r.Entries {
        if winningEntry == nil {
            winningEntry = entry
        } else if entry.Score > winningEntry.Score {
            winningEntry = entry
        }
    }
    room.Announce(fmt.Sprintf("%s wins the roll-off with a score of %d!", winningEntry.User.Username, winningEntry.Score), false)
}

func NewRollOff(w http.ResponseWriter, r *http.Request) {
    rollingUser := ParseUser(r)
    entry := &RollOffEntry{User: rollingUser, Score: rand.Intn(100) + 1}

    rolloff := &RollOff{Id: randomString(20), Open: true}
    rolloff.AddEntry(entry)
    go rolloff.Cycle()

    rolloffs = append(rolloffs, rolloff)

    for elem := room.Users.Front(); elem != nil; elem = elem.Next() {
        go func(e *list.Element) {
            var tName string
            u := e.Value.(*User)
            if u == rollingUser {
                tName = "templates/roll-off/rolling-user.html"
            } else {
                tName = "templates/roll-off/other-users.html"
            }
            t := template.Must(template.ParseFile(tName))
            m := NewMessage("system", "", "system")
            t.Execute(m, entry)
            u.c <- m
        }(elem)
    }
}

func EnterRollOff(w http.ResponseWriter, r *http.Request) {
    id := r.URL.Path[len("/roll-off-entry/"):]
    fmt.Println("User wishes to enter rolloff ", id)
    rollingUser := ParseUser(r)
    entry := &RollOffEntry{User: rollingUser, Score: rand.Intn(100) + 1}

    for _, r := range rolloffs {
        fmt.Println("Checking rolloff ", r.Id)
        if r.Id == id {
            r.AddEntry(entry)
            for elem := room.Users.Front(); elem != nil; elem = elem.Next() {
                go func(e *list.Element) {
                    var tName string
                    u := e.Value.(*User)
                    if u == rollingUser {
                        tName = "templates/roll-off/user-joins.html"
                    } else {
                        tName = "templates/roll-off/other-user-joins.html"
                    }
                    t := template.Must(template.ParseFile(tName))
                    m := NewMessage("system", "", "system")
                    t.Execute(m, entry)
                    u.c <- m
                }(elem)
            }
        }
    }
}

// Renders markdown in submitted messages.
func Render(raw []byte) []byte {
    htmlFlags := 0
    htmlFlags |= blackfriday.HTML_USE_XHTML
    htmlFlags |= blackfriday.HTML_USE_SMARTYPANTS
    htmlFlags |= blackfriday.HTML_SMARTYPANTS_FRACTIONS
    htmlFlags |= blackfriday.HTML_SMARTYPANTS_LATEX_DASHES
    renderer := blackfriday.HtmlRenderer(htmlFlags, "", "")

    // set up the parser
    extensions := 0
    extensions |= blackfriday.EXTENSION_NO_INTRA_EMPHASIS
    extensions |= blackfriday.EXTENSION_TABLES
    extensions |= blackfriday.EXTENSION_FENCED_CODE
    extensions |= blackfriday.EXTENSION_STRIKETHROUGH
    extensions |= blackfriday.EXTENSION_SPACE_HEADERS

    escaped := new(bytes.Buffer)
    template.HTMLEscape(escaped, raw)
    rendered := blackfriday.Markdown(escaped.Bytes(), renderer, extensions)
    enabled := true
    fmt.Println("Raw message:")
    fmt.Println(string(raw))
    if enabled {
        fmt.Println("Rendered message:")
        fmt.Println(string(rendered))
        return rendered
    }
    return raw
}

var GetEmbed = func() func(string) (string, bool) {
    client := new(http.Client)
    key := "83518a4c0f8f11e186fe4040d3dc5c07"
    templateString := "http://api.embed.ly/1/oembed?key=" + key + "&url=%s" + "&maxwidth=800"
    return func(url string) (string, bool) {
        requestUrl := fmt.Sprintf(templateString, url)
        res, err := client.Get(requestUrl)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error in GetEmbed: %s\n", err)
            return "", false
        }
        defer res.Body.Close()
        contents, err := ioutil.ReadAll(res.Body)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error in GetEmbed 2: %s\n", err)
            return "", false
        }
        var raw map[string] interface{}
        json.Unmarshal(contents, &raw)
        fmt.Println(raw)
        if html, contains := raw["html"]; contains {
            return html.(string), true
            // return `<div style="width: 800px;">` + html.(string) + `</div>`, true
        }
        return url, false
    }
}()

func main() {
    runtime.GOMAXPROCS(8)
    port := "0.0.0.0:8000"
    room = NewRoom()
    rolloffs = make([]*RollOff, 0)
    staticDir := http.Dir("/projects/go/chat/static")
    staticServer := http.FileServer(staticDir)
    homeTemplate = template.Must(template.ParseFile("templates/index.html"))

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
