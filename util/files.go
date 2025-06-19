package util

import (
	"fmt"
	"os"
)


func IsValidPath(path Path) bool{
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err){
			return false
		}
		fmt.Println(err)
		return false
	} else {
		return true
	}
}
