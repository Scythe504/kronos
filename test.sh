#!/bin/bash

while read -r error_code; do
  # Doing some work based on the error_code received
  case "$error_code" in 
    0 ) echo "OK:$error_code:SUCCESS"; exit 0;;
    127 ) echo "ERR:$error_code:Not Found";;
    1 ) echo "ERR:$error_code:General error";;
    * ) echo "ERR:$error_code:Unrecoverable";; # Unrecoverable
  esac
done