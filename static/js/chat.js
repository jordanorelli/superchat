var Chat = (function($) {
  var $loginElements;           // elements shown when the user is logged out
  var $usernameField;           // allows the user to input a desired username
  var $loginErrors;             // an element where we will place login errors

  var $chatElements;            // elements shown when the user is logged in
  var $usernameDisplay;         // shows the user their current username
  var $messageContainer;        // element to hold messages as they arrive
  var messageWrapperTemplate;   // a Mustache template to render the outer
                                // message context.  That is the username and,
                                // in the future, user controls.
  var messageBodyTemplate;      // a Mustache template to render the innter
                                // messsage context.  That is, the body of the
                                // message and a timestamp.
  var recentMessageClass;       // class to help us select the div contianing
                                // the most recent messages, to assist in
                                // grouping consecutive messages from the same
                                // user

  var $composeMessageField;     // allows the user to input a chat message
  var $logoutButton;            // element to which a logout function is bound
  var $chatErrors;              // an element where we will place chat errors

  var username = '';            // holds the currently logged in username.  If this
  var lastMessageUsername = ''; // retains the username of the last message, so
                                // that if the same user sends subsequent
                                // messages we put them within the same div and
                                // don't repeat their username
  var loggedIn = false;
  var errorCount = 0;
  var debug = true;

  // Scrolls the window to the bottom of the chat dialogue.
  var scrollToEnd = function() {
    $(document).scrollTop($(document).height() + 500);
  };

  // A primitve UI state controller. Call with true to show the "logged in" UI;
  // call with false to show the "logged out" UI.
  var setChatDisplay = function(enabled) {
    $loginElements.toggle(!enabled);
    $chatElements.toggle(enabled);
  };

  // Performs an ajax call to log the user in.  Sends an empty POST request
  // with the username in the request URL.
  var login = function() {
    var desiredUsername = $.trim($usernameField.val());
    if(desiredUsername.match(/\s/)){
      handleError($loginErrors, "", "desired username " + desiredUsername + " contains invalid whitespace");
      return false;
    };
    if(desiredUsername.length >= 25) {
      handleError($loginErrors, "", "That username is too long.  Max is 24 characters.");
      return false;
    }

    $.ajax({
      type: "POST",
      url: "/login",
      contentType: "application/json; charset=utf-8",
      dataType: "json",
      data: JSON.stringify({
        username: desiredUsername
      }),
      async: true,
      cache: false,
      timeout: 5000,
      success: function(data){
        username = desiredUsername;
        console.log("Logged in as: ", username)
        loggedIn = true;
        $usernameDisplay.text(username);
        setChatDisplay(true);
        $loginErrors.toggle(false);
        $composeMessageField.focus();
        displayMessages(data);
        poll();
      },
      error: function(XMLHttpRequest, textStatus, errorThrown) {
        if(debug){
          console.log(XMLHttpRequest, textStatus, errorThrown);
        }
        handleError($loginErrors, textStatus, errorThrown);
        return false;
      }
    });
  };

  // Performs an ajax call to log the user out.  Sends an empty DELETE request
  // with the username in the request URL.
  var logout = function() {
    $.ajax({
      type: "DELETE",
      url: "/login",
      async: true,
      cache: false,
      timeout: 30000,
      success: function(data){
        // do nothing, we logout in complete
      },
      error: function(XMLHttpRequest, textStatus, errorThrown) {
        // do nothing, we logout in complete even if we fail
        handleErrors($loginErrors, textStatus, errorThrown);
      },
      complete: function() {
        logoutClient()
      }
    });
  };

  // performs all the local actions needed to log a user out
  // this will get called without logout ajax call when a session is expired
  var logoutClient = function() {
    setChatDisplay(false);
    username = '';
    console.log("Cleared username");
    loggedIn = false;
    $usernameField.val('');
    $usernameField.focus();
  };

  // Given a list of messages, appends them to the $messageContainer element,
  // according to the Mustache template defined as messageTemplate.
  var displayMessages = function(messages) {
    console.log("Displaying messages ", messages);
    $(messages).each(function(){
      console.log("Checking username...");
      if(this.Username != lastMessageUsername) {
        console.log("New username...");
        $("."+recentMessageClass).removeClass(recentMessageClass);
        $messageContainer.append(renderMessageWrapper(this));
        console.log("added message wrapper");
      }
      $("."+recentMessageClass).append(renderMessageBody(this));
      console.log("added message body");
      lastMessageUsername = this.Username;
    });
    scrollToEnd();
  };

  var clearMessages = function() {
    $messageContainer.empty();
  };

  var formatTime = function(timestamp) {
    var date = new Date(Date.UTC(timestamp.Year, timestamp.Month, timestamp.Day,
                                 timestamp.Hour, timestamp.Minute,
                                 timestamp.Second));
    return date.toString().split(' ')[4];
  }

  var renderMessageWrapper = function(message) {
    return Mustache.to_html(messageWrapperTemplate, {
      username: message.Username,
      msgtype: message.MsgType
    });
  };

  var renderMessageBody = function(message) {
    return Mustache.to_html(messageBodyTemplate, {
      formattedTime: formatTime(message.Timestamp),
      body: message.Body
    });
  };

  // Given an input element and a button element, disables the button if the
  // input field is empty.
  var setButtonBehavior = function($inputField, $submitButton){
    var value = $.trim($inputField.val());
    if(value){
      $submitButton.removeAttr("disabled");
    } else {
      $submitButton.attr("disabled", "disabled");
    }
  };

  // processes a send message request.  The message is sent as a POST request,
  // with the message text defined in the POST body.
  var sendMessage = function(event) {
    // var $this = $(this);
    var message = $.trim($composeMessageField.val());
    // $this.attr("disabled", "disabled");
    $composeMessageField.blur();
    $composeMessageField.attr("disabled", "disabled");

    if (message[0] === '/') {
      $composeMessageField.val("");
      $composeMessageField.removeAttr("disabled");
      $composeMessageField.focus();
      // $this.removeAttr("disabled");
      event.preventDefault();
      event.stopPropagation();
      runCmd(message);
      return false;
    }

    $.ajax({
      type: 'POST',
      url: '/feed',
      contentType: "application/json; charset=utf-8",
      dataType: "json",
      data: JSON.stringify({
        body: message
      }),
      success: function() {
        $composeMessageField.val("");
        $chatErrors.toggle(false);
      },
      error: function(XMLHttpRequest, textStatus, errorThrown) {
        console.log("ERROR!");
        console.log(XMLHttpRequest, textStatus, errorThrown);
        handleError($chatErrors, textStatus, errorThrown);
      },
      complete: function() {
        $composeMessageField.removeAttr("disabled");
        $composeMessageField.focus();
        // $this.removeAttr("disabled");
      }
    });

    event.preventDefault();
    event.stopPropagation();
    return false;
  };

  // sends a GET request for new messages.  This function will recurse indefinitely.
  var poll = function() {
    if (!loggedIn) {
      return false;
    }
    $.ajax({
      type: "GET",
      url: "/feed",
      async: true,
      timeout: 1200000,
      success: function(data) {
        console.log("Got some messages.  My username:", username);
        errorCount = 0;
        displayMessages(data);
      },
      error: function(XMLHttpRequest, textStatus, errorThrown) {
        errorCount += 1;
        console.log("ERROR!");
        console.log(XMLHttpRequest, textStatus, errorThrown);
        handleError($chatErrors, textStatus, errorThrown);
      },
      complete: function() {
        if(errorCount < 3) {
          poll();
        }
      }
    });
  };

  var roll = function() {
    $.ajax({
      type: "POST",
      url: "/roll",
      async: true,
      timeout: 60000,
      error: function(XMLHttpRequest, textStatus, errorThrown) {
        console.log(XMLHttpRequest, textStatus, errorThrown);
      }
    });
  }

  var rollOff = function() {
    $.ajax({
      type: "POST",
      url: "/roll-off",
      async: true,
      timeout: 60000,
      success: function(data) {
        console.log(data);
      },
      error: function(XMLHttpRequest, textStatus, errorThrown) {
        console.log(XMLHttpRequest, textStatus, errorThrown);
      }
    });
  }

  var getUsers = function() {
    $.ajax({
      type: "GET",
      url: "/users",
      async: true,
      timeout: 60000,
      success: function(data) {
        var body = "Users online: ";
        body += $.map(data, function(user){return user.Username;}).join(', ');
        displayMessages([{
          Body: body,
          MsgType: "system"
        }]);
      },
      error: function(XMLHttpRequest, textStatus, errorThrown) {
        console.log(XMLHttpRequest, textStatus, errorThrown);
      }
    });
  };

  // display our chat errors
  // if the session has timed out, boot them
  // if there is a network error, assume the server is down, boot them
  var handleError = function($errorElement, textStatus, errorThrown) {
    console.log(textStatus, errorThrown);
    if(errorThrown === 'Authentication failed') {
      logoutClient();
      $loginErrors.text('Authentication failed! Perhaps your session expired.');
      $loginErrors.toggle(true);
    } else if (errorThrown === 'Not found' || errorThrown === 'timeout') {
      logoutClient();
      $loginErrors.text('Chat server can not be found. Perhaps it is down, or you have no network connection.');
      $loginErrors.toggle(true);
    } else {
      if (errorThrown ==='')
        errorThrown = 'Unable to contact server';
      $errorElement.text(errorThrown);
      $errorElement.toggle(true);
    }
  };

  var cmds = {
    '/clear': clearMessages,
    '/users': getUsers,
    '/roll': roll,
    '/roll-off': rollOff
  };

  var runCmd = function(cmd) {
    if(cmd in cmds){
      cmds[cmd]();
    } else {
      displayMessages([{
        Body: "Unknown command: " + cmd,
        MsgType: "error"
      }]);
    }
  };

  // Our main setup function.  This function performs no dom manipulation directly,
  // so the layout of your page is preserved after it is called. Accepts a
  // config object as its only argument, which is used to specify jQuery
  // selectors of to bind event listeners to, as well as a Mustache template to
  // dictate how a message should be formatted.
  var buildChatWindow = function(config) {
    $chatElements = $(config.chatElements);
    $messageContainer = $(config.messageContainer);
    $logoutButton = $(config.logoutButton);
    $loginElements = $(config.loginElements);
    $loginErrors = $(config.loginErrors);
    $composeMessageField = $(config.composeMessageField);
    $usernameField = $(config.usernameField);
    $usernameDisplay = $(config.usernameDisplay);
    $chatErrors = $(config.chatErrors);
    messageBodyTemplate = config.messageBodyTemplate;
    messageWrapperTemplate = config.messageWrapperTemplate;
    recentMessageClass = config.recentMessageClass;

    $logoutButton.click(function(event) {
      logout();
      event.preventDefault();
    });

    $composeMessageField.keydown(function(event) {
      if(event.keyCode == 13 && !event.shiftKey) {
        if($.trim($composeMessageField.val())){
          sendMessage(event);
        } else {
          return false;
        }
      } else if(event.keyCode == 76 && event.ctrlKey) {
        clearMessages();
      }
    });

    $usernameField.keydown(function(event) {
      if(event.keyCode == 13 ){
        if($.trim($usernameField.val())){
          login();
        }
      }
    });

    $(window).unload(function(event){
      logout();
    });

    console.log("Build chat window complete!  Username: ", username);
  };

  // set a short default timeout
  // we set this for most get requests that need to be longer
  $.ajaxSetup({ timeout: 3000 } );

  return {
    buildChatWindow: buildChatWindow,
    getUsername: function() {
      return username;
    }
  };
})($);
