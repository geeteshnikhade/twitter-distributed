package main

import (
	"log"
	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	pb "twitter-distributed/utils/ProtoDef"
	"google.golang.org/grpc/reflection"
	"fmt"
	"errors"
	"sync"
	"strconv"
	"os"
	"time"
)

const (
	NORMAL = iota
	VIEWCHANGE
	RECOVERING
)

// server is used to implement helloworld.GreeterServer.

type server struct {
	mu             sync.Mutex // Lock to protect shared access to this peer's state
	peers          []string   // Ports of all peers
	peerRPC        [3]pb.GreeterClient
	me             int      // this peer's index into peers[]
	currentView    int      // what this peer believes to be the current active view
	status         int      // the server's current status (NORMAL, VIEWCHANGE or RECOVERING)
	lastNormalView int      // the latest view which had a NORMAL status
	log            []string // the log of "commands"
	commitIndex    int      // all log entries <= commitIndex are considered to have been committed.
	opNo           int
}

// SayHello implements helloworld.GreeterServer

//userdata
var userdata = make(map[string]User)

type User struct {
	username string
	password string
	tweets   []tweet
	follows  map[string]bool
}

type tweet struct {
	text string
}

//userdataend

//debugfuntion
var debugon = true //if set to true debug outputs are printed

//Function to print debug outputs if debugon=true
func debugPrint(text string) {
	if (debugon) {
		fmt.Println(text)
	}
}

//test function
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.Name}, nil
}

//test function2
func (s *server) SayHelloAgain(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello again friend " + in.Name}, nil
}

//registeruser function
func (s *server) Register(ctx context.Context, in *pb.Credentials) (*pb.RegisterReply, error) {

	if in.Broadcast == true {
		//index, view, ok := s.Start(in.String())
		//println(in.String())
		index, _, ok := s.Start(in.String())
		if ok == false {
			debugPrint("Error: Discarding last Register operation")
			return &pb.RegisterReply{Message: "Error: Backend Replication system is down."}, errors.New("backend replication system is down")
		}
		in.Broadcast = false
		count := 0
		for i, rpccaller := range s.peerRPC {
			if i != s.me {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_, err := rpccaller.Register(ctx, in)
				if err != nil {
					fmt.Printf("Debug: Server %d was unreachable \n", i)
					//fmt.Printf("Debug: Error was %s \n",err)
				} else {
					count++
				}
			}
		}
		if count >= len(s.peers)/2 {
			fmt.Println("Debug: Replication on backup servers acheived")
			s.commitIndex = index
		} else {
			fmt.Printf("Error: Replication failed, replicated only on %d servers", count+1)
			//TODO: Return here?
		}
	}

	usrname := in.Uname
	pwd := in.Pwd

	_, ok := userdata[usrname]
	if ok {
		debugPrint("Debug: User already exists")
		return &pb.RegisterReply{Message: "User already exists"}, errors.New("user already exists")
	}
	usr := User{username: usrname, password: pwd}
	usr.follows = make(map[string]bool)
	userdata[usrname] = usr
	fmt.Printf("Debug: User %s successfully added \n",usrname)
	return &pb.RegisterReply{Message: "User succesfully added"}, nil
}

func (s *server) Login(ctx context.Context, in *pb.Credentials) (*pb.LoginReply, error) {
	user, ok := userdata[in.Uname]
	if !ok {
		debugPrint("Debug: No such user")
		return &pb.LoginReply{Status: false}, errors.New("no such User")
	}
	if in.Pwd == user.password {
		return &pb.LoginReply{Status: true}, nil
	} else {
		debugPrint("Debug: Wrong password")
		return &pb.LoginReply{Status: false}, errors.New("wrong password")
	}
}

func (s *server) AddTweet(ctx context.Context, in *pb.AddTweetRequest) (*pb.AddTweetReply, error) {

	// Will be Broadcasted to all the other servers
	//println(in.String())
	if in.Broadcast == true {

		//Starting Prepare
		index, _, ok := s.Start(in.String())
		if ok == false {
			debugPrint("Error: Discarding last Add Tweet operation")
			return &pb.AddTweetReply{Status: false}, errors.New("backend replication system down")
		}

		//Majority servers agreed in the prepare phase
		//Setting broadcast false so that the backup servers don't send this message further
		in.Broadcast = false
		count := 0
		for i, rpccaller := range s.peerRPC {
			if i != s.me {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				//Add Tweet RPC calls to all the backup servers
				_, err := rpccaller.AddTweet(ctx, in)
				if err != nil {
					fmt.Printf("Debug: Server %d was unreachable \n", i)
					//fmt.Printf("Debug: Error was %s \n",err)
				} else {
					//Counting the number of successful commits
					count++
				}
			}
		}

		//Majority of backups successfully performed the operation
		if count >= len(s.peers)/2 {
			fmt.Printf("Debug: Tweet '%s' successfuly added to the Majority servers {Replication achieved} \n",in.TweetText)
			s.commitIndex = index

		} else {
			//RPC to majority servers failed
			fmt.Printf("Debug: Adding tweet to all servers failed, tweet added only to %d servers", count+1)
			//TODO: Return here?
		}
	}


	user, ok := userdata[in.Username]
	if !ok {
		debugPrint("Debug: No such user")
		return &pb.AddTweetReply{Status: false}, errors.New("No such User")
	}
	//Add new tweet and update in the Map
	newTweet := tweet{text: in.TweetText}
	user.tweets = append(user.tweets, newTweet)
	userdata[in.Username] = user
	fmt.Printf("Debug: Successfully added tweet '%s' for %s \n",in.TweetText,in.Username)
	return &pb.AddTweetReply{Status: true}, nil
}

func (s *server) OwnTweets(ctx context.Context, in *pb.OwnTweetsRequest) (*pb.OwnTweetsReply, error) {
	user, ok := userdata[in.Username]
	if (!ok) {
		debugPrint("Debug: No such user")
		return nil, errors.New("no such user")
	}
	response := pb.OwnTweetsReply{}
	for _, i := range user.tweets {
		tweetToAdd := pb.Tweet{Text: i.text}
		response.TweetList = append(response.TweetList, &tweetToAdd)
	}
	//debugPrint("Debug: your tweets")
	//fmt.Println(response)
	return &response, nil
}

func (s *server) UserExists(ctx context.Context, in *pb.UserExistsRequest) (*pb.UserExistsReply, error) {
	username := in.Username
	_, ok := userdata[username]
	if !ok {
		debugPrint("Debug: No such user")
		return &pb.UserExistsReply{Status: false}, errors.New("no such user exists")
	} else {
		return &pb.UserExistsReply{Status: true}, nil
	}
}

func (s *server) DeleteUser(ctx context.Context, in *pb.Credentials) (*pb.DeleteReply, error) {

	// Will be Broadcasted to all the other servers
	println(in.String())
	if in.Broadcast == true {

		//Starting Prepare
		index, _, ok := s.Start(in.String())
		if ok == false {
			debugPrint("Debug: Discarding last Delete operation")
			return &pb.DeleteReply{DeleteStatus: false}, errors.New("backend replication system down")
		}

		//Majority servers agreed in the prepare phase
		//Setting broadcast false so that the backup servers don't send this message further
		in.Broadcast = false
		count := 0
		for i, rpccaller := range s.peerRPC {
			if i != s.me {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				//Delete User RPC calls to all the backup servers
				_, err := rpccaller.DeleteUser(ctx, in)
				if err != nil {
					fmt.Printf("Debug: Server %d was unreachable \n", i)
					//fmt.Printf("Debug: Error was %s \n",err)
				} else {
					//Counting the number of successful commits
					count++
				}
			}
		}

		//Majority of backups successfully performed the operation
		if count >= len(s.peers)/2 {
			fmt.Printf("Debug: User %s deleted from Majority servers \n",in.Uname)
			s.commitIndex = index

			// Master itself performing the operation
			//debugPrint("Deleting User: " + in.Uname + "Account")
			//delete(userdata, in.Uname)
			//return &pb.DeleteReply{DeleteStatus: false}, nil

		} else {
			//RPC to majority servers failed
			fmt.Printf("Debug: User Deletion failed, User deleted only from %d servers", count+1)
			//TODO: Return here?
		}
	}

	//debugPrint("Deleting User: " + in.Uname + "'s Account")
	delete(userdata, in.Uname)
	debugPrint("Debug: Successfully deleted user "+in.Uname)
	return &pb.DeleteReply{DeleteStatus: true}, nil

}

func (s *server) FollowUser(ctx context.Context, in *pb.FollowUserRequest) (*pb.FollowUserResponse, error) {

	//println(in.String())
	// Will be Broadcasted to all the other servers
	if in.Broadcast == true {

		//Starting Prepare
		index, _, ok := s.Start(in.String())
		if ok == false {
			debugPrint("Debug: Discarding last Follow User operation")
			return &pb.FollowUserResponse{FollowStatus : false}, errors.New("Backend Replication system down")
		}

		//Majority servers agreed in the prepare phase
		//Setting broadcast false so that the backup servers don't send this message further
		in.Broadcast = false
		count := 0
		for i, rpccaller := range s.peerRPC {
			if i != s.me {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				//Add Tweet RPC calls to all the backup servers
				_, err := rpccaller.FollowUser(ctx, in)
				if err != nil {
					fmt.Printf("Debug: Server %d was unreachable \n", i)
					//fmt.Printf("Debug: Error was %s \n",err)
				} else {
					//Counting the number of successful commits
					count++
				}
			}
		}

		//Majority of backups successfully performed the operation
		if count >= len(s.peers)/2 {
			fmt.Printf("Debug: User %s followed User %s  replicated on Majority servers {Replication achieved} \n",in.SelfUsername,in.ToFollowUsername)
			s.commitIndex = index

		} else {
			//RPC to majority servers failed
			fmt.Printf("Debug: Following user on all servers failed, tweet added only to %d servers", count+1)
			//TODO: Return here?
		}
	}

	//debugPrint("User: " + in.SelfUsername + " has requested to follow: " + in.ToFollowUsername)
	//Getting user from user data map and adding the new user to be followed
	user, ok := userdata[in.SelfUsername]
	if !ok {
		return &pb.FollowUserResponse{FollowStatus: false}, errors.New("Debug: Selfuser does not exist")
	}
	_, ok2 := userdata[in.ToFollowUsername]
	if !ok2 {
		return &pb.FollowUserResponse{FollowStatus: false}, errors.New("Debug: ToFollow user does not exist")
	}
	//fmt.Println("value of ok2", ok2, in.ToFollowUsername)
	user.follows[in.ToFollowUsername] = true
	fmt.Printf("Debug: %s follows user %s successfully mapped",in.SelfUsername,in.ToFollowUsername)
	return &pb.FollowUserResponse{FollowStatus: true}, nil

}

func (s *server) UsersToFollow(ctx context.Context, in *pb.UsersToFollowRequest) (*pb.UsersToFollowResponse, error) {
	response := &pb.UsersToFollowResponse{}
	//Get the user from our Map
	user, isUserPresent := userdata[in.Username]
	//fmt.Println("Self Username: ", user.username)
	if isUserPresent {
		for eachUser := range userdata {
			_, ok := user.follows[eachUser]
			//fmt.Println("Each User: ", eachUser)
			if ok == false && eachUser != user.username {
				//Preparing a list of all the users to follow list
				response.UsersToFollowList = append(response.UsersToFollowList, &pb.User{Username: eachUser})
			}
		}
		return response, nil
	} else {
		return nil, errors.New("User does not exist!")
	}
}

func (s *server) GetFriendsTweets(ctx context.Context, in *pb.GetFriendsTweetsRequest) (*pb.GetFriendsTweetsResponse, error) {
	response := &pb.GetFriendsTweetsResponse{}

	//Get the user from our Map
	user, isUserPresent := userdata[in.Username]
	if isUserPresent {
		for eachFollowedUser := range user.follows {
			//Iterate through all the Followed Users
			eachFollowedUserData := userdata[eachFollowedUser]
			userAllTweets := &pb.UsersAllTweets{}
			userAllTweets.Username = &pb.User{Username: eachFollowedUser}
			//println(eachFollowedUser)
			//Append all the tweets ap per the User
			for _, eachUserTweet := range eachFollowedUserData.tweets {
				//println(eachUserTweet.text)
				userAllTweets.Tweets = append(userAllTweets.Tweets, &pb.Tweet{Text: eachUserTweet.text})
			}
			//Append all of current Followed users data into the response
			response.FriendsTweets = append(response.FriendsTweets, userAllTweets)
		}
	}

	println(response.FriendsTweets)
	return response, nil
}

//This function is used by the FE server to talk to any server and get a response of who the primary is
func (s *server) WhoIsPrimary(ctx context.Context, in *pb.WhoisPrimaryRequest) (*pb.WhoIsPrimaryResponse, error) {
	primaryIndex := GetPrimary(s.currentView, len(s.peers))
	if primaryIndex > -1 && primaryIndex < len(s.peers) {
		return &pb.WhoIsPrimaryResponse{Index: int32(primaryIndex)}, nil
	}
	return &pb.WhoIsPrimaryResponse{Index: -1}, errors.New("Debug: Index of primary out of bounds")
}

//used to rpc and check if connection is alive
func (s *server) HeartBeat(ctx context.Context, in *pb.HeartBeatRequest) (*pb.HeartBeatResponse, error) {
	return &pb.HeartBeatResponse{IsAlive: true, CurrentView: int32(s.currentView)}, nil
}

//internal function call
func GetPrimary(view int, nservers int) int {
	return view % nservers
}

//prepare is used to synchronize servers
func (srv *server) Prepare(ctx context.Context, args *pb.PrepareArgs) (reply *pb.PrepareReply, err error) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	reply = &pb.PrepareReply{}
	reply.View = int32(srv.currentView)
	reply.Success = false
	if int(args.View) < srv.currentView {
		return
	}

	if int(args.Index) <= srv.commitIndex {
		return
	}
	if int(args.PrimaryCommit) > srv.commitIndex {
		srv.commitIndex = int(args.PrimaryCommit)
	}

	if int(args.Index) != srv.opNo+1 || int(args.View) > srv.currentView {
		fmt.Println("Debug:~~~~~~~~~~~~~~Server needs to recover~~~~~~~~~~~~~")
		//log.Fatal("Debug: Server needs to recover")
		srv.status = RECOVERING
		PrimaryIndex := GetPrimary(int(args.View), len(srv.peers))
		RecoveryInArgs := pb.RecoveryArgs{
			View:   args.View,
			Server: int32(srv.me),
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		RecoveryOutArgs, err := srv.peerRPC[PrimaryIndex].Recovery(ctx, &RecoveryInArgs)

		if err == nil {
			if RecoveryOutArgs.Success {
				srv.log = RecoveryOutArgs.Entries
				srv.commitIndex = int(RecoveryOutArgs.PrimaryCommit)
				srv.currentView = int(RecoveryOutArgs.View)

				//StartRestoring Data
				userdata = make(map[string]User)
				for _, recoveredUser := range RecoveryOutArgs.Data {
					//recover user credentials
					userToRecover := User{username: recoveredUser.Username, password: recoveredUser.Password}
					userToRecover.follows = make(map[string]bool)
					//recover tweets for user
					for _, tweetToRecover := range recoveredUser.TweetList {
						recreatedTweet := tweet{text: tweetToRecover.Text}
						userToRecover.tweets = append(userToRecover.tweets, recreatedTweet)
					}
					//recover users followlist
					for _, followerToRecover := range recoveredUser.Follows {
						userToRecover.follows[followerToRecover] = true
					}
					//add user to user data
					userdata[userToRecover.username] = userToRecover
				}

				srv.status = NORMAL
				srv.opNo = len(srv.log) - 1
				//srv.commitIndex=int(args.PrimaryCommit)
				reply.Success = true
				//fmt.Println(userdata)			Todo: Discuss if this should be logged
				fmt.Println("Debug: Recovery Completed")
				return reply, nil
			} else {
				return reply, errors.New("Error: Error while recovering")

			}
		} else {
			return reply, errors.New("Error: Error while recovering")
		}
	}
	//srv.commitIndex=args.PrimaryCommit
	if int(args.Index) == len(srv.log) {
		srv.log = append(srv.log, args.Entry)
		srv.opNo = srv.opNo + 1
		srv.commitIndex = int(args.PrimaryCommit)
		reply.Success = true
		return
	}
	return

}

//Start calls prepare and returns index to commit on. In this case with >1/2 prepare's start does not immediately write the commit index.
//The commit index is updated after > 1/2 Prepare+RPC
func (srv *server) Start(command string) (index int, view int, ok bool) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	// do not process command if status is not NORMAL
	// and if i am not the primary in the current view
	if srv.status != NORMAL {
		debugPrint("Debug: Request can't be processed as the Server is not in NORMAL mode")
		return -1, srv.currentView, false
	} else if GetPrimary(srv.currentView, len(srv.peers)) != srv.me {
		//Check if you're the Primary
		debugPrint("Debug: Illegal request made to a Non-primary server")
		return -1, srv.currentView, false
	}

	//In case of failure, the command is still added to the log so we tell backup the new index
	srv.log = append(srv.log, command)
	srv.opNo = srv.opNo + 1
	count := 0

	//Calling all backups
	for i, rpcEndPoint := range srv.peerRPC {
		if i != srv.me {
			pointer := i
			inArgs := &pb.PrepareArgs{
				View:          int32(srv.currentView),
				PrimaryCommit: int32(srv.commitIndex),
				Index:         int32(srv.opNo),
				Entry:         command,
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			outArgs, err := rpcEndPoint.Prepare(ctx, inArgs)
			if err == nil {
				if outArgs.Success == true {
					count = count + 1
				} else {
					fmt.Printf("Error: Prepare rpc to Server %d failed \n", pointer)
					//fmt.Printf("error is : %s \n", err)
				}
			} else {
				fmt.Printf("Error: Prepare rpc to Server %d failed \n", pointer)
				//fmt.Printf("error is : %s \n", err)
			}

		}

	}

	//Determine the number of severs for majority
	length := len(srv.peers)

	//Check if majority calls have returned, consider Primary as committed
	if count >= length/2 {
		ok = true
		index = srv.opNo
	} else {
		index = -1
		ok = false
		debugPrint("Fatal: Back-end Replication Down (Majority of Servers unresponsive)")
	}
	return index, view, ok
}

func (srv *server) Recovery(ctx context.Context, args *pb.RecoveryArgs) (reply *pb.RecoveryReply, err error) {

	reply = &pb.RecoveryReply{}
	reply.View = int32(srv.currentView)
	reply.Entries = srv.log
	reply.PrimaryCommit = int32(srv.commitIndex)
	reply.Success = true

	//Start Initializing data
	for _, value := range userdata {
		//add users credentials to userobject
		userToAdd := &pb.UserData{Username: value.username, Password: value.password}

		//add users tweets to userobject
		for _, userTweet := range value.tweets {
			tweetToAdd := &pb.Tweet{Text: userTweet.text}
			userToAdd.TweetList = append(userToAdd.TweetList, tweetToAdd)
		}

		//add users followlist to userobject
		for userFollows := range value.follows {
			userToAdd.Follows = append(userToAdd.Follows, userFollows)
		}

		//Now finally after building the userobject append this to the recoveryReply
		reply.Data = append(reply.Data, userToAdd)

	}

	return reply, nil

	return
}

func (srv *server) PromptViewChange(ctx context.Context, args *pb.PromptViewChangeArgs) (reply *pb.PromptViewChangeReply, err error) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	newView := int(args.NewView)
	newPrimary := GetPrimary(newView, len(srv.peers))

	if newPrimary != srv.me { //only primary of newView should do view change
		return
	} else if newView <= srv.currentView {
		return
	}
	fmt.Println("Debug: ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~")
	fmt.Printf("Debug: Looks like the primary is down. Trying to become the new primary.. \n")
	vcArgs := &pb.ViewChangeArgs{
		View: int32(newView),
	}
	vcReplyChan := make(chan *pb.ViewChangeReply, len(srv.peers))
	// send ViewChange to all servers including myself
	for i := 0; i < len(srv.peers); i++ {
		go func(server int) {
			val := server
			var reply *pb.ViewChangeReply
			//ok := srv.peers[server].Call("server.ViewChange", vcArgs, &reply)
			// fmt.Printf("node-%d (nReplies %d) received reply ok=%v reply=%v\n", srv.me, nReplies, ok, r.reply)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			//fmt.Println(val)
			reply, err := srv.peerRPC[val].ViewChange(ctx,vcArgs)
			if err==nil && reply.Success == true {
				vcReplyChan <- reply
			} else {
				vcReplyChan <- nil
			}
		}(i)
	}

	// wait to receive ViewChange replies
	// if view change succeeds, send StartView RPC
	go func() {
		var successReplies []*pb.ViewChangeReply
		var nReplies int
		majority := len(srv.peers)/2 + 1
		for r := range vcReplyChan {
			nReplies++
			if r != nil && r.Success {
				successReplies = append(successReplies, r)
			}
			if nReplies == len(srv.peers) || len(successReplies) == majority {
				break
			}
		}
		ok, log := srv.determineNewViewLog(successReplies)
		if !ok {
			return
		}
		svArgs := &pb.StartViewArgs{
			View: vcArgs.View,
			Log:  log,
		}
		// send StartView to all servers including myself
		for i := 0; i < len(srv.peers); i++ {
			go func(server int) {
				//fmt.Printf("Debug: node-%d sending StartView v=%d to node-%d\n", srv.me, svArgs.View, server)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_, _ = srv.peerRPC[server].StartView(ctx, svArgs)
			}(i)
		}
	}()
	return &pb.PromptViewChangeReply{Success:true}, nil
}

func (srv *server) determineNewViewLog(successReplies []*pb.ViewChangeReply) (ok bool,log []string)  {
	// Your code here
	lenSucess:=len(successReplies)
	Majority:=(len(srv.peers)-1)/2+1
	if(lenSucess<Majority){
		ok=false
		return
	}
	Index:=0
	MaxView:=0
	MaxLength:=0
	for i,reply :=  range successReplies{
		if(int(reply.LastNormalView)>MaxView){
			Index=i
			MaxView=int(reply.LastNormalView)
			MaxLength=len(reply.Log)
		}
		if(int(reply.LastNormalView)==MaxView && len(reply.Log)>MaxLength){
			Index=i
			MaxView=int(reply.LastNormalView)
			MaxLength=len(reply.Log)
		}
	}
	log =successReplies[Index].Log
	ok=true
	return ok, log
}

func (srv *server) StartView(ctx context.Context, args *pb.StartViewArgs) (reply *pb.StartViewReply, err error) {
	if(srv.currentView>int(args.View)){
		return &pb.StartViewReply{}, errors.New("start view failed")
	}
	fmt.Printf("Debug: Starting new view \n")
	srv.currentView=int(args.View)
	//srv.log=args.Log
	srv.status=NORMAL
	//srv.opNo=len(srv.log)-1
	fmt.Printf("Debug: We have a new primary Server %d \n",GetPrimary(int(args.View),len(srv.peers)))
	return &pb.StartViewReply{}, nil

}

func (srv *server) ViewChange(ctx context.Context, args *pb.ViewChangeArgs) (reply *pb.ViewChangeReply,err error) {
	// Your code here
	reply = &pb.ViewChangeReply{}
	if(int(args.View)<=srv.currentView){
		reply.Success=false
		return reply, errors.New("Debug: Server View greater than ViewChange Request")
	}
	fmt.Printf("Debug: We need a new Primary, Server %d is trying to become the primary \n",GetPrimary(int(args.View),len(srv.peers)));
	fmt.Println("Debug: Starting view change")
	reply.LastNormalView=int32(srv.currentView)
	reply.Log=srv.log
	reply.Success=true
	srv.lastNormalView=srv.currentView
	srv.currentView=int(args.View)
	srv.status=VIEWCHANGE
	return reply, nil
}

func main() {

	//fetch ServerID to know index in peers list
	ServerID, err := strconv.Atoi(os.Args[1])
	if err != nil {
		//handle Error
		fmt.Println("Debug: Invalid ServerID, Exit", err)
		os.Exit(2)
	}

	//set up backend server for VSReplication
	srv := &server{
		me:             ServerID,
		currentView:    0,
		lastNormalView: 0,
		status:         NORMAL,
		opNo:           0,
	}

	srv.log = append(srv.log, "")
	srv.peers = append(srv.peers, ":50051")
	srv.peers = append(srv.peers, ":50052")
	srv.peers = append(srv.peers, ":50053")

	// Error if user enters some random server
	if ServerID >= len(srv.peers) || ServerID < 0 {
		fmt.Printf("Debug: ServerID %d is not supported. Server Exiting \n", ServerID)
		os.Exit(2)
	}

	//Set up listener on your own port
	lis, err := net.Listen("tcp", srv.peers[srv.me])
	if err != nil {
		log.Fatalf("Debug: failed to listen, server could not be started: %v", err)
		os.Exit(2)
	} else {
		fmt.Printf("Woo hoo! server %d started \n", srv.me)
	}

	//Set up rpccaller objects to other peer servers
	for index, port := range srv.peers {
		conn, err := grpc.Dial(port, grpc.WithInsecure())
		if err != nil {
			fmt.Printf("did not connect to port %s \n",port)
			//log.Fatal("Error: %s",err)
		}
		defer conn.Close()
		//c := pb.NewGreeterClient(conn)
		srv.peerRPC[index] = pb.NewGreeterClient(conn)
	}

	//This code can probably be used to test if all servers are up using heartbeat. Needs fixes, commented for now

	//ch := make(chan pb.GreeterClient,len(srv.peers))
	//fmt.Printf("Channel created \n")
	//for i,rpcclient := range srv.peerRPC{
	//	fmt.Println("reached1 i=",i)
	//	if(i!=srv.me) {
	//		fmt.Printf("started this")
	//		ch <- rpcclient
	//		fmt.Println("reached this")
	//	}
	//	fmt.Println("reached 2")
	//}
	//fmt.Printf("Debug: Waiting for other servers to come up \n %d of 3 servers are up \n",len(ch)-1)
	//
	//for len(ch)!=0{
	//	rpcclient := <-ch
	//	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	//	defer cancel()
	//	_, err := rpcclient.HeartBeat(ctx, &pb.HeartBeatRequest{})
	//	if(err!=nil){
	//		ch<-rpcclient
	//		fmt.Println(err)
	//	}
	//	fmt.Printf("Debug: %d of 3 servers are up \n",len(ch)-1 )
	//	time.Sleep(3*time.Second)
	//}

	// This is a test for connected-ness between Server's for rpc. Each server tries to contact every other sever
	for index, rpccaller := range srv.peerRPC {
		if index != srv.me {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			reply, err := rpccaller.WhoIsPrimary(ctx, &pb.WhoisPrimaryRequest{})
			if err != nil {
				fmt.Printf("Could not connect to Server %d \n", index)
			} else {
				fmt.Printf("Server %d replied that the primary is %d \n", index, reply.Index)
			}
		}
	}

	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, srv)
	// Register reflection service on gRPC server.
	reflection.Register(s)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
