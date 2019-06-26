function connectWS(url, proto) {
        return new Promise((resolve, reject) => {
                var ws = new WebSocket(url, proto)
                ws.onopen = () => {
                        ws.onopen = null
                        ws.onerror = null
                        resolve(ws)
                }
                ws.onerror = (event) => reject(event)
        })
}

function login(username) {
        return new Promise((resolve, reject) => {
                connectWS("ws://" + location.host + "/chat", "demo-chat").then((ws) => {
                        ws.send(username)
                        resolve(ws)
                }, reject)
        })
}

var e = (id) => document.getElementById(id)
var txt = (str) => document.createTextNode(str)
var elem = (t) => document.createElement(t)
window.onload = () => {
        var logindiv = e('logindiv')
        var loginform = e('loginform')
        var chatdiv = e('chatdiv')
        var chatform = e('chatform')
        loginform.onsubmit = (ev) => {
                logindiv.innerHTML = 'Loading. . .'
                login((new FormData(loginform)).get('username')).then((ws) =>{
                        chatform.onsubmit = (ev) => {
                                ws.send((new FormData(chatform)).get('body'))
                                chatform.reset()
                                return false
                        }
                        ws.onmessage = (ev) => {
                                var msg = JSON.parse(ev.data)
                                chatdiv.appendChild(elem('br'))
                                var sender = elem('b')
                                sender.appendChild(txt(msg.sender+' '))
                                chatdiv.appendChild(sender)
                                chatdiv.appendChild(txt(msg.body))
                        }
                        ws.onclose = (ev) => {
                                console.log(ev)
                                ws.onmessage({data:JSON.stringify({
                                        sender: 'server',
                                        body: 'You have disconnected',
                                })})
                        }
                        chatdiv.hidden = false
                        logindiv.hidden = true
                }, (err) => {
                        console.log(err)
                        logindiv.innerHTML = 'Login failed, please check console for more information'
                })
                return false
        }
}
