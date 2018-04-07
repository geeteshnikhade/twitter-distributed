package main

import (
	"fmt"

	"net/http"
)


var userdata = make(map[string]User)

type User struct {
	username string
	password string
	tweets []tweet
	follows map[string]bool
}

type tweet struct {
	text string
}

var debugon = true //if set to true debug outputs are printed

//Function to print debug outputs if debugon=true
func debugPrint(text string){
	if(debugon){
		fmt.Println(text)
	}
}

//function to add user to data on registration
func addUser(usrname string, pwd string) int  {
	_, ok := userdata[usrname]
	if(ok){
		debugPrint("Debug: User already exists")
		return 0
	}
	usr := User{username:usrname,password:pwd}
	usr.follows = make(map[string]bool)
	userdata[usrname] = usr
	debugPrint("Debug: User added")
	return 1
}

//Delete a user account
func deleteUser(username string) int  {
	//TODO: for later stages, we'll have to add Locks here
	debugPrint("Deleting User: " + username +"Account")
	delete(userdata,username)
	return 1
}

//Returns users password
func getPassword(usrname string) (bool, string){

	user, ok := userdata[usrname]
	if(!ok){
		debugPrint("No such user")
		return false, "No such User"
	}
	return true, user.password

}

func deleteCookie(w http.ResponseWriter){
	cookie := http.Cookie{Name: "username", MaxAge: -1}
	http.SetCookie(w, &cookie)
	debugPrint("Debug:Cookie Deleted")
	return
}