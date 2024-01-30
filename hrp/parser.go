package hrp

import (
	builtinJSON "encoding/json"
	"fmt"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/httprunner/funplugin"
	"github.com/httprunner/funplugin/fungo"
	"github.com/maja42/goval"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/httprunner/httprunner/v4/hrp/internal/builtin"
	"github.com/httprunner/httprunner/v4/hrp/internal/code"
)

func newParser() *Parser {
	return &Parser{}
}

type Parser struct {
	plugin funplugin.IPlugin // plugin is used to call functions
}

func buildURL(baseURL, stepURL string, queryParams url.Values) (fullUrl *url.URL) {
	uStep, err := url.Parse(stepURL)
	if err != nil {
		log.Error().Str("stepURL", stepURL).Err(err).Msg("[buildURL] parse url failed")
		return nil
	}

	defer func() {
		// append query params
		if paramStr := queryParams.Encode(); paramStr != "" {
			if uStep.RawQuery == "" {
				uStep.RawQuery = paramStr
			} else {
				uStep.RawQuery = uStep.RawQuery + "&" + paramStr
			}
		}

		// Splice suffixes as needed "/"
		isSuffix := strings.HasSuffix(stepURL, "/") || (strings.HasSuffix(baseURL, "/") && stepURL == "")
		if uStep.RawQuery == "" && isSuffix {
			uStep.Path = strings.TrimRight(uStep.Path, "/") + "/"
		}

		fullUrl = uStep
	}()

	// step url is absolute url
	if uStep.Host != "" {
		return uStep
	}

	// step url is relative, based on base url
	uConfig, err := url.Parse(baseURL)
	if err != nil {
		log.Error().Str("baseURL", baseURL).Err(err).Msg("[buildURL] parse url failed")
		return
	}

	// merge url
	uStep.Scheme = uConfig.Scheme
	uStep.Host = uConfig.Host
	uStep.Path = path.Join(uConfig.Path, uStep.Path)
	return uStep
}

func (p *Parser) ParseHeaders(rawHeaders map[string]string, variablesMapping map[string]interface{}) (map[string]string, error) {
	parsedHeaders := make(map[string]string)
	headers, err := p.Parse(rawHeaders, variablesMapping)
	if err != nil {
		return rawHeaders, err
	}
	for k, v := range headers.(map[string]interface{}) {
		parsedHeaders[k] = convertString(v)
	}
	return parsedHeaders, nil
}

func convertString(raw interface{}) string {
	if str, ok := raw.(string); ok {
		return str
	}
	if float, ok := raw.(float64); ok {
		// f: avoid conversion to exponential notation
		return strconv.FormatFloat(float, 'f', -1, 64)
	}
	// convert to string
	return fmt.Sprintf("%v", raw)
}

func (p *Parser) Parse(raw interface{}, variablesMapping map[string]interface{}) (interface{}, error) {
	rawValue := reflect.ValueOf(raw)
	switch rawValue.Kind() {
	case reflect.String:
		// json.Number
		if rawValue, ok := raw.(builtinJSON.Number); ok {
			return parseJSONNumber(rawValue)
		}
		// other string
		value := rawValue.String()
		value = strings.TrimSpace(value)
		return p.ParseString(value, variablesMapping)
	case reflect.Slice:
		parsedSlice := make([]interface{}, rawValue.Len())
		for i := 0; i < rawValue.Len(); i++ {
			parsedValue, err := p.Parse(rawValue.Index(i).Interface(), variablesMapping)
			if err != nil {
				return raw, err
			}
			parsedSlice[i] = parsedValue
		}
		return parsedSlice, nil
	case reflect.Map: // convert any map to map[string]interface{}
		parsedMap := make(map[string]interface{})
		for _, k := range rawValue.MapKeys() {
			parsedKey, err := p.ParseString(k.String(), variablesMapping)
			if err != nil {
				return raw, err
			}
			v := rawValue.MapIndex(k)
			parsedValue, err := p.Parse(v.Interface(), variablesMapping)
			if err != nil {
				return raw, err
			}

			key := convertString(parsedKey)
			parsedMap[key] = parsedValue
		}
		return parsedMap, nil
	default:
		// other types, e.g. nil, int, float, bool
		return builtin.TypeNormalization(raw), nil
	}
}

func parseJSONNumber(raw builtinJSON.Number) (value interface{}, err error) {
	if strings.Contains(raw.String(), ".") {
		// float64
		value, err = raw.Float64()
	} else {
		// int64
		value, err = raw.Int64()
	}
	if err != nil {
		return nil, errors.Wrap(code.ParseError,
			fmt.Sprintf("parse json number failed: %v", err))
	}
	return value, nil
}

const (
	regexVariable     = `[a-zA-Z_]\w*`    // variable name should start with a letter or underscore
	regexFunctionName = `[a-zA-Z_]\w*`    // function name should start with a letter or underscore
	regexNumber       = `^-?\d+(\.\d+)?$` // match number, e.g. 123, -123, 1.23, -1.23
)

var (
	regexCompileVariable = regexp.MustCompile(fmt.Sprintf(`\$\{(%s)\}|\$(%s)`, regexVariable, regexVariable))     // parse ${var} or $var
	regexCompileFunction = regexp.MustCompile(fmt.Sprintf(`\$\{(%s)\(([\$\w\.\-/\s=,]*)\)\}`, regexFunctionName)) // parse ${func1($a, $b)}
	regexCompileNumber   = regexp.MustCompile(regexNumber)                                                        // parse number
)

// ParseString parse string with variables
func (p *Parser) ParseString(raw string, variablesMapping map[string]interface{}) (interface{}, error) {
	matchStartPosition := 0
	parsedString := ""
	remainedString := raw

	for matchStartPosition < len(raw) {
		// locate $ char position
		startPosition := strings.Index(remainedString, "$")
		if startPosition == -1 { // no $ found
			// append remained string
			parsedString += remainedString
			break
		}

		// found $, check if variable or function
		matchStartPosition += startPosition
		parsedString += remainedString[0:startPosition]
		remainedString = remainedString[startPosition:]

		// Notice: notation priority
		// $$ > ${func($a, $b)} > $var

		// search $$, use $$ to escape $ notation
		if strings.HasPrefix(remainedString, "$$") { // found $$
			matchStartPosition += 2
			parsedString += "$"
			remainedString = remainedString[2:]
			continue
		}

		// search function like ${func($a, $b)}
		funcMatched := regexCompileFunction.FindStringSubmatch(remainedString)
		if len(funcMatched) == 3 {
			funcName := funcMatched[1]
			argsStr := funcMatched[2]
			arguments, err := parseFunctionArguments(argsStr)
			if err != nil {
				return raw, errors.Wrap(code.ParseFunctionError, err.Error())
			}
			parsedArgs, err := p.Parse(arguments, variablesMapping)
			if err != nil {
				return raw, err
			}

			result, err := p.callFunc(funcName, parsedArgs.([]interface{})...)
			if err != nil {
				log.Error().Str("funcName", funcName).Interface("arguments", arguments).
					Err(err).Msg("call function failed")
				return raw, errors.Wrap(code.CallFunctionError, err.Error())
			}
			log.Info().Str("funcName", funcName).Interface("arguments", arguments).
				Interface("output", result).Msg("call function success")

			if funcMatched[0] == raw {
				// raw_string is a function, e.g. "${add_one(3)}", return its eval value directly
				return result, nil
			}

			// raw_string contains one or many functions, e.g. "abc${add_one(3)}def"
			matchStartPosition += len(funcMatched[0])
			parsedString += convertString(result)
			remainedString = raw[matchStartPosition:]
			log.Debug().
				Str("parsedString", parsedString).
				Int("matchStartPosition", matchStartPosition).
				Msg("[parseString] parse function")
			continue
		}

		// search variable like ${var} or $var
		varMatched := regexCompileVariable.FindStringSubmatch(remainedString)
		if len(varMatched) == 3 {
			var varName string
			if varMatched[1] != "" {
				varName = varMatched[1] // match ${var}
			} else {
				varName = varMatched[2] // match $var
			}
			varValue, ok := variablesMapping[varName]
			if !ok {
				return raw, errors.Wrap(code.VariableNotFound,
					fmt.Sprintf("variable %s not found", varName))
			}

			if fmt.Sprintf("${%s}", varName) == raw || fmt.Sprintf("$%s", varName) == raw {
				// raw string is a variable, $var or ${var}, return its value directly
				return varValue, nil
			}

			matchStartPosition += len(varMatched[0])
			parsedString += convertString(varValue)
			remainedString = raw[matchStartPosition:]
			log.Debug().
				Str("parsedString", parsedString).
				Int("matchStartPosition", matchStartPosition).
				Msg("[parseString] parse variable")
			continue
		}

		parsedString += remainedString
		break
	}

	return parsedString, nil
}

// callFunc calls function with arguments
// only support return at most one result value
func (p *Parser) callFunc(funcName string, arguments ...interface{}) (interface{}, error) {
	// call with plugin function
	if p.plugin != nil {
		if p.plugin.Has(funcName) {
			return p.plugin.Call(funcName, arguments...)
		}
		commonName := fungo.ConvertCommonName(funcName)
		if p.plugin.Has(commonName) {
			return p.plugin.Call(commonName, arguments...)
		}
	}

	// get builtin function
	function, ok := builtin.Functions[funcName]
	if !ok {
		return nil, fmt.Errorf("function %s is not found", funcName)
	}
	fn := reflect.ValueOf(function)

	// call with builtin function
	return fungo.CallFunc(fn, arguments...)
}

// merge two variables mapping, the first variables have higher priority
func mergeVariables(variables, overriddenVariables map[string]interface{}) map[string]interface{} {
	if overriddenVariables == nil {
		return variables
	}
	if variables == nil {
		return overriddenVariables
	}

	mergedVariables := make(map[string]interface{})
	for k, v := range overriddenVariables {
		mergedVariables[k] = v
	}
	for k, v := range variables {
		if fmt.Sprintf("${%s}", k) == v || fmt.Sprintf("$%s", k) == v {
			// e.g. {"base_url": "$base_url"}
			// or {"base_url": "${base_url}"}
			continue
		}

		mergedVariables[k] = v
	}
	return mergedVariables
}

// merge two map, the first map have higher priority
func mergeMap(m, overriddenMap map[string]string) map[string]string {
	if overriddenMap == nil {
		return m
	}
	if m == nil {
		return overriddenMap
	}

	mergedMap := make(map[string]string)
	for k, v := range overriddenMap {
		mergedMap[k] = v
	}
	for k, v := range m {
		mergedMap[k] = v
	}
	return mergedMap
}

// merge two validators slice, the first validators have higher priority
func mergeValidators(validators, overriddenValidators []interface{}) []interface{} {
	if validators == nil {
		return overriddenValidators
	}
	if overriddenValidators == nil {
		return validators
	}
	var mergedValidators []interface{}
	validators = append(validators, overriddenValidators...)
	for _, validator := range validators {
		flag := true
		for _, mergedValidator := range mergedValidators {
			if validator.(Validator).Check == mergedValidator.(Validator).Check {
				flag = false
				break
			}
		}
		if flag {
			mergedValidators = append(mergedValidators, validator)
		}
	}
	return mergedValidators
}

// merge two slices, the first slice have higher priority
func mergeSlices(slice, overriddenSlice []string) []string {
	if slice == nil {
		return overriddenSlice
	}
	if overriddenSlice == nil {
		return slice
	}

	for _, value := range overriddenSlice {
		if !builtin.Contains(slice, value) {
			slice = append(slice, value)
		}
	}
	return slice
}

var eval = goval.NewEvaluator()

// literalEval parse string to number if possible
func literalEval(raw string) (interface{}, error) {
	raw = strings.TrimSpace(raw)

	// return raw string if not number
	if !regexCompileNumber.Match([]byte(raw)) {
		return raw, nil
	}

	// eval string to number
	result, err := eval.Evaluate(raw, nil, nil)
	if err != nil {
		log.Error().Err(err).Msgf("[literalEval] eval %s failed", raw)
		return raw, err
	}
	return result, nil
}

func parseFunctionArguments(argsStr string) ([]interface{}, error) {
	argsStr = strings.TrimSpace(argsStr)
	if argsStr == "" {
		return []interface{}{}, nil
	}

	// split arguments by comma
	args := strings.Split(argsStr, ",")
	arguments := make([]interface{}, len(args))
	for index, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}

		// parse argument to number if possible
		arg, err := literalEval(arg)
		if err != nil {
			return nil, err
		}
		arguments[index] = arg
	}

	return arguments, nil
}

func (p *Parser) ParseVariables(variables map[string]interface{}) (map[string]interface{}, error) {
	parsedVariables := make(map[string]interface{})
	var traverseRounds int

	for len(parsedVariables) != len(variables) {
		for varName, varValue := range variables {
			// skip parsed variables
			if _, ok := parsedVariables[varName]; ok {
				continue
			}

			// extract variables from current value
			extractVarsSet := extractVariables(varValue)

			// check if reference variable itself
			// e.g.
			// variables = {"token": "abc$token"}
			// variables = {"key": ["$key", 2]}
			if _, ok := extractVarsSet[varName]; ok {
				log.Error().Interface("variables", variables).Msg("[parseVariables] variable self reference error")
				return variables, errors.Wrap(code.ParseVariablesError,
					fmt.Sprintf("variable self reference: %v", varName))
			}

			// check if reference variable not in variables mapping
			// e.g.
			// {"varA": "123$varB", "varB": "456$varC"} => $varC not defined
			// {"varC": "${sum_two($a, $b)}"} => $a, $b not defined
			var undefinedVars []string
			for extractVar := range extractVarsSet {
				if _, ok := variables[extractVar]; !ok { // not in variables mapping
					undefinedVars = append(undefinedVars, extractVar)
				}
			}
			if len(undefinedVars) > 0 {
				log.Error().Interface("undefinedVars", undefinedVars).Msg("[parseVariables] variable not defined error")
				return variables, errors.Wrap(code.ParseVariablesError,
					fmt.Sprintf("variable not defined: %v", undefinedVars))
			}

			parsedValue, err := p.Parse(varValue, parsedVariables)
			if err != nil {
				continue
			}
			parsedVariables[varName] = parsedValue
		}
		traverseRounds += 1
		// check if circular reference exists
		if traverseRounds > len(variables) {
			log.Error().Msg("[parseVariables] circular reference error, break infinite loop!")
			return variables, errors.Wrap(code.ParseVariablesError, "circular reference")
		}
	}

	return parsedVariables, nil
}

type variableSet map[string]struct{}

func extractVariables(raw interface{}) variableSet {
	rawValue := reflect.ValueOf(raw)
	switch rawValue.Kind() {
	case reflect.String:
		return findallVariables(rawValue.String())
	case reflect.Slice:
		varSet := make(variableSet)
		for i := 0; i < rawValue.Len(); i++ {
			for extractVar := range extractVariables(rawValue.Index(i).Interface()) {
				varSet[extractVar] = struct{}{}
			}
		}
		return varSet
	case reflect.Map:
		varSet := make(variableSet)
		for _, key := range rawValue.MapKeys() {
			value := rawValue.MapIndex(key)
			for extractVar := range extractVariables(value.Interface()) {
				varSet[extractVar] = struct{}{}
			}
		}
		return varSet
	default:
		// other types, e.g. nil, int, float, bool
		return make(variableSet)
	}
}

func findallVariables(raw string) variableSet {
	matchStartPosition := 0
	remainedString := raw
	varSet := make(variableSet)

	for matchStartPosition < len(raw) {
		// locate $ char position
		startPosition := strings.Index(remainedString, "$")
		if startPosition == -1 { // no $ found
			return varSet
		}

		// found $, check if variable or function
		matchStartPosition += startPosition
		remainedString = remainedString[startPosition:]

		// Notice: notation priority
		// $$ > $var

		// search $$, use $$ to escape $ notation
		if strings.HasPrefix(remainedString, "$$") { // found $$
			matchStartPosition += 2
			remainedString = remainedString[2:]
			continue
		}

		// search variable like ${var} or $var
		varMatched := regexCompileVariable.FindStringSubmatch(remainedString)
		if len(varMatched) == 3 {
			var varName string
			if varMatched[1] != "" {
				varName = varMatched[1] // match ${var}
			} else {
				varName = varMatched[2] // match $var
			}
			varSet[varName] = struct{}{}

			matchStartPosition += len(varMatched[0])
			remainedString = raw[matchStartPosition:]
			continue
		}

		break
	}

	return varSet
}
