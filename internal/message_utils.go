package internal

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/oleiade/reflections"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/tidwall/gjson"
)

var (
	//go:embed en_US.json
	messageJsonBytes []byte

	messageJson       gjson.Result
	rootCommandName   = "akamai"
	jsonPathSeparator = "."
	flagKey           = "flag"
	subCommandKey     = "subCommand"
	placeHolderRegex  = "{{(.*?)}}"
	edgeGridError     = "edgeGridError"
	fallbackKey       = "fallback"
	messagesKey       = "messages"
)

//Get global errors for given key
func GetGlobalErrorMessage(key string) string {
	return getMessageForJsonPathOrFallback(strings.Join([]string{rootCommandName, Global, key}, jsonPathSeparator))
}

//Get edge grid errors for given key
func GetEdgeGridErrorMessage(key string) string {
	return getMessageForJsonPathOrFallback(strings.Join([]string{rootCommandName, Global, edgeGridError, key}, jsonPathSeparator))
}

// Return message for given key under command.
func GetMessageForKey(baseCmdPath *cobra.Command, key string) string {
	jsonPath := getJsonPathForCommand(strings.Join([]string{baseCmdPath.CommandPath(), key}, " "))
	return getMessageForJsonPathOrFallback(jsonPath)
}

// Get different type of message for flag
func GetErrorMessageForFlag(cmd *cobra.Command, errorType, flagKeyInJson string) string {

	jsonPath := getJsonPathForCommand(cmd.CommandPath())
	switch errorType {
	case Missing:
		jsonPath = strings.Join([]string{jsonPath, flagKey, Missing, flagKeyInJson}, jsonPathSeparator)
	case Invalid:
		jsonPath = strings.Join([]string{jsonPath, flagKey, Invalid, flagKeyInJson}, jsonPathSeparator)
	}

	log.Debugf("Get message for json path [%s], error type - [%s], flag key in json - [%s]", jsonPath, errorType, flagKeyInJson)
	return getMessageForJsonPathOrFallback(jsonPath)
}

func GetApiErrorMessagesForCommand(cmd *cobra.Command, apiError ApiError, subResource, operation, responseCode string) []string {
	jsonPathForCommand := getJsonPathForCommand(cmd.CommandPath())
	if (apiError.ClientIp != Empty && apiError.ServerIp != Empty && apiError.RequestId != Empty) || (responseCode == "401" && apiError.Code != Empty) {

		if (apiError.Status == 400 || apiError.Status == 401) && apiError.Detail != Empty {
			PrintError(GetEdgeGridErrorMessage(getErrorJsonKeyForDetail(apiError.Detail)) + "\n")
		}

		if apiError.Status == 404 {
			PrintError(GetEdgeGridErrorMessage("resourceNotFound") + "\n")
		}

		if responseCode == "401" && apiError.Code != Empty {
			PrintError(GetEdgeGridErrorMessage("notAuthorized") + "\n")
		}
		PrintError(GetGlobalErrorMessage("initEdgeRc") + "\n")
		// Get equivalent exit status code for corresponding http status code
		statusCode := GetHttpExitCode(responseCode)
		os.Exit(statusCode)
	}
	parentErrorKey := getErrorJsonKeyForErrorType(apiError.Type)

	if len(apiError.Errors) != 0 {
		return GetApiSubErrorMessagesForCommand(cmd, apiError.Errors, parentErrorKey, subResource, operation)
	} else {
		errorPath := getJsonPathForCommand(strings.Join([]string{jsonPathForCommand, subResource, operation, parentErrorKey}, " "))
		errorMessage := getMessageForJsonPathOrFallback(errorPath)
		return []string{getReplacedPlaceholderMessage(apiError, errorMessage)}
	}
}

// Get All the error messages for api sub errors
func GetApiSubErrorMessagesForCommand(cmd *cobra.Command, apiSubError []ApiSubError, parentErrorKey, subResource, operation string) []string {
	jsonPathForCommand := getJsonPathForCommand(cmd.CommandPath())
	var errorMessages = make([]string, len(apiSubError))

	for i, subError := range apiSubError {
		subErrorKey := getErrorJsonKeyForErrorType(subError.Type)
		errorPath := getJsonPathForCommand(strings.Join([]string{jsonPathForCommand, subResource, operation, parentErrorKey, subErrorKey}, " "))

		/*Custom logic starts here*/

		//Pulsar object sometimes contains same error type for different objects, currently not able to figure out how to show those messages differently for different objects
		//One other possible solution is show generic message
		// For now this is done to only support submit test run
		// First custom logic
		if strings.Contains("resourceNotFound,resourceInDeletedState", subErrorKey) && checkIfMessageExist(errorPath+jsonPathSeparator+subError.RequestField+strings.Title(subErrorKey)) {

			errorMessage := getMessageForJsonPathOrFallback(errorPath + jsonPathSeparator + subError.RequestField + strings.Title(subErrorKey))
			// Replace placeholder values in string from json if there are any
			errorMessages[i] = getReplacedPlaceholderMessage(subError, errorMessage)
			continue
		}

		// Second custom logic
		if strings.Contains("associationNotFound", subErrorKey) {

			if subError.RequirementId != 0 && checkIfMessageExist(errorPath+jsonPathSeparator+"requirementId"+"TestSuiteId"+strings.Title(subErrorKey)) {
				errorMessage := getMessageForJsonPathOrFallback(errorPath + jsonPathSeparator + "requirementId" + "TestSuiteId" + strings.Title(subErrorKey))
				errorMessages[i] = getReplacedPlaceholderMessage(subError, errorMessage)
				continue
			}

			if subError.ConfigVersionId != 0 && checkIfMessageExist(errorPath+jsonPathSeparator+"configVersionId"+"TestSuiteId"+strings.Title(subErrorKey)) {
				errorMessage := getMessageForJsonPathOrFallback(errorPath + jsonPathSeparator + "configVersionId" + "TestSuiteId" + strings.Title(subErrorKey))
				errorMessages[i] = getReplacedPlaceholderMessage(subError, errorMessage)
				continue
			}

			if subError.TestSuiteId != 0 && checkIfMessageExist(errorPath+jsonPathSeparator+"testSuiteId"+"TestCaseId"+strings.Title(subErrorKey)) {
				errorMessage := getMessageForJsonPathOrFallback(errorPath + jsonPathSeparator + "testSuiteId" + "TestCaseId" + strings.Title(subErrorKey))
				errorMessages[i] = getReplacedPlaceholderMessage(subError, errorMessage)
				continue
			}
		}

		/*Custom logic ends here*/

		errorMessage := getMessageForJsonPathOrFallback(errorPath)
		// Replace placeholder values in string from json if there are any
		errorMessages[i] = getReplacedPlaceholderMessage(subError, errorMessage)
	}
	return errorMessages
}

func getErrorJsonKeyForErrorType(errorType string) string {

	str := strings.Split(errorType, jsonPathSeparator)
	var jsonPath = make([]string, len(str))
	for i2, s := range str {
		if i2 == 0 {
			jsonPath[i2] = s
		} else {
			jsonPath[i2] = strings.Title(s)
		}
	}
	return strings.Join(jsonPath, "")
}

func getErrorJsonKeyForDetail(errorType string) string {

	str := strings.Split(errorType, " ")
	var jsonPath = make([]string, len(str))
	for i2, s := range str {
		if i2 == 0 {
			jsonPath[i2] = strings.ToLower(s)
		} else {
			jsonPath[i2] = strings.Title(s)
		}
	}
	return strings.Join(jsonPath, "")
}

// Replace placeholder values in string from json if there are any
func getReplacedPlaceholderMessage(error interface{}, errorMessage string) string {

	for _, str := range GetPlaceHoldersInString(errorMessage, placeHolderRegex) {
		value, _ := reflections.GetField(error, strings.Title(str))
		errorMessage = strings.ReplaceAll(errorMessage, fmt.Sprintf("{{%s}}", str), fmt.Sprintf("%v", value))

	}
	return errorMessage
}

// Return json path for given command chain, e.g. -  `test-center test-suite view` converted to akamai.testCenter.testSuite.view
func getJsonPathForCommand(cmdString string) string {
	log.Debugf("Get json path for command [%s]", cmdString)
	givenString := strings.Fields(cmdString)
	var jsonPath = make([]string, len(givenString))

	for i, str := range givenString {
		var dashRemovedString = make([]string, len(str))
		for i2, dashedString := range strings.Split(str, "-") {
			if i2 == 0 {
				dashRemovedString[i2] = dashedString
			} else {
				dashRemovedString[i2] = strings.Title(dashedString)
			}
		}
		jsonPath[i] = strings.Join(dashRemovedString, "")
	}

	convertedString := strings.Join(jsonPath, jsonPathSeparator)
	if strings.Contains(convertedString, rootCommandName) {
		return convertedString
	}

	return strings.Join([]string{rootCommandName, convertedString}, jsonPathSeparator)
}

// standard function to get message from json for given json path
func checkIfMessageExist(jsonPath string) bool {
	message := gjson.Get(messageJson.String(), jsonPath)
	log.Debugf("Message for json path [%s] : [%s]", jsonPath, message.String())
	return message.Exists()
}

// standard function to get message from json for given json path
func getMessageForJsonPathOrFallback(jsonPath string) string {
	message := gjson.Get(messageJson.String(), jsonPath)
	if message.Exists() && message.Type == gjson.String {
		log.Debugf("Message for json path [%s] : [%s]", jsonPath, message.String())
		return message.String()
	} else {
		log.Infof("Message for json path [%s] : [%s]", jsonPath, message.String())
		log.Debugf("Message is not configured for jsonPath [%s]", jsonPath)
		return gjson.Get(messageJson.String(), rootCommandName+jsonPathSeparator+fallbackKey).String()
	}
}

// GetErrorMessageForSubArgument different type of message for invalid argument
func GetErrorMessageForSubArgument(cmd *cobra.Command, errorType, subCommandKeyInJson string) string {

	jsonPath := getJsonPathForCommand(cmd.CommandPath())
	switch errorType {
	case Missing:
		jsonPath = strings.Join([]string{jsonPath, subCommandKey, Missing, subCommandKeyInJson}, jsonPathSeparator)
	case Invalid:
		jsonPath = strings.Join([]string{jsonPath, subCommandKey, Invalid, subCommandKeyInJson}, jsonPathSeparator)
	}

	log.Debugf("Get message for json path [%s], error type - [%s], flag key in json - [%s]", jsonPath, errorType, subCommandKeyInJson)
	return getMessageForJsonPathOrFallback(jsonPath)
}

// GetServiceMessage different type of service messages based on commands and messageType
func GetServiceMessage(cmd *cobra.Command, messageType string, subresource string, jsonKey string) string {

	jsonPath := getJsonPathForCommand(cmd.CommandPath())
	switch messageType {
	case MessageTypeSpinner:
		jsonPath = strings.Join([]string{jsonPath, messagesKey, MessageTypeSpinner, jsonKey}, jsonPathSeparator)
	case MessageTypeDisplay:
		jsonPath = strings.Join([]string{jsonPath, messagesKey, MessageTypeDisplay, jsonKey}, jsonPathSeparator)
	case MessageTypeTestCmdSpinner:
		jsonPath = strings.Join([]string{jsonPath, messagesKey, subresource, MessageTypeSpinner, jsonKey}, jsonPathSeparator)
	}

	log.Debugf("Get message for json path [%s], message type - [%s], subresource type - [%s], flag key in json - [%s]", jsonPath, messageType, subresource, jsonKey)
	return getMessageForJsonPathOrFallback(jsonPath)
}

// Initialize message file
func init() {
	messageJson = gjson.ParseBytes(messageJsonBytes)
}
