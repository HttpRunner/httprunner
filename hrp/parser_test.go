package hrp

import (
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBuildURL(t *testing.T) {
	var preparedURL *url.URL

	preparedURL = buildURL("https://postman-echo.com", "/get", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/get") {
		t.Fatal()
	}
	preparedURL = buildURL("https://postman-echo.com", "get", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/get") {
		t.Fatal()
	}
	preparedURL = buildURL("https://postman-echo.com/", "/get", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/get") {
		t.Fatal()
	}

	preparedURL = buildURL("https://postman-echo.com", "/get/", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/get/") {
		t.Fatal()
	}
	preparedURL = buildURL("https://postman-echo.com", "get/", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/get/") {
		t.Fatal()
	}
	preparedURL = buildURL("https://postman-echo.com/", "/get/", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/get/") {
		t.Fatal()
	}

	preparedURL = buildURL("https://postman-echo.com/abc/", "/get?a=1&b=2", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/abc/get?a=1&b=2") {
		t.Fatal()
	}
	preparedURL = buildURL("https://postman-echo.com/abc", "get?a=1&b=2", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/abc/get?a=1&b=2") {
		t.Fatal()
	}

	// omit query string in base url
	preparedURL = buildURL("https://postman-echo.com/abc?x=6&y=9", "/get?a=1&b=2", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/abc/get?a=1&b=2") {
		t.Fatal()
	}

	preparedURL = buildURL("", "https://postman-echo.com/get", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/get") {
		t.Fatal()
	}

	// notice: step request url > config base url
	preparedURL = buildURL("https://postman-echo.com", "https://httpbin.org/get", nil)
	if !assert.Equal(t, preparedURL.String(), "https://httpbin.org/get") {
		t.Fatal()
	}

	// websocket url
	preparedURL = buildURL("wss://ws.postman-echo.com/raw", "", nil)
	if !assert.Equal(t, preparedURL.String(), "wss://ws.postman-echo.com/raw") {
		t.Fatal()
	}

	preparedURL = buildURL("wss://ws.postman-echo.com", "/raw", nil)
	if !assert.Equal(t, preparedURL.String(), "wss://ws.postman-echo.com/raw") {
		t.Fatal()
	}

	preparedURL = buildURL("wss://ws.postman-echo.com/raw", "ws://echo.websocket.events", nil)
	if !assert.Equal(t, preparedURL.String(), "ws://echo.websocket.events") {
		t.Fatal()
	}

	preparedURL = buildURL("", "https://postman-echo.com/get/", nil)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/get/") {
		t.Fatal()
	}

	// notice: step request url > config base url
	preparedURL = buildURL("https://postman-echo.com", "https://httpbin.org/get/", nil)
	if !assert.Equal(t, preparedURL.String(), "https://httpbin.org/get/") {
		t.Fatal()
	}

	// websocket url
	preparedURL = buildURL("wss://ws.postman-echo.com/raw/", "", nil)
	if !assert.Equal(t, preparedURL.String(), "wss://ws.postman-echo.com/raw/") {
		t.Fatal()
	}

	preparedURL = buildURL("wss://ws.postman-echo.com", "/raw/", nil)
	if !assert.Equal(t, preparedURL.String(), "wss://ws.postman-echo.com/raw/") {
		t.Fatal()
	}

	preparedURL = buildURL("wss://ws.postman-echo.com/raw/", "ws://echo.websocket.events/", nil)
	if !assert.Equal(t, preparedURL.String(), "ws://echo.websocket.events/") {
		t.Fatal()
	}

	preparedURL = buildURL("wss://ws.postman-echo.com/raw", "ws://echo.websocket.events/", nil)
	if !assert.Equal(t, preparedURL.String(), "ws://echo.websocket.events/") {
		t.Fatal()
	}

	preparedURL = buildURL("wss://ws.postman-echo.com/raw/", "ws://echo.websocket.events", nil)
	if !assert.Equal(t, preparedURL.String(), "ws://echo.websocket.events") {
		t.Fatal()
	}

	queryParams := url.Values{}
	queryParams.Add("c", "3")
	queryParams.Add("d", "4")
	preparedURL = buildURL("https://postman-echo.com/", "/get/", queryParams)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/get?c=3&d=4") {
		t.Fatal()
	}
	preparedURL = buildURL("https://postman-echo.com/abc", "get?a=1&b=2", queryParams)
	if !assert.Equal(t, preparedURL.String(), "https://postman-echo.com/abc/get?a=1&b=2&c=3&d=4") {
		t.Fatal()
	}
}

func TestRegexCompileVariable(t *testing.T) {
	testData := []string{
		"$var1",
		"${var1}",
		"$v",
		"var_1$_v",
		"${var_1}#XYZ",
		"func1($var_1, $var_3)",
	}

	for _, expr := range testData {
		varMatched := regexCompileVariable.FindStringSubmatch(expr)
		if !assert.Len(t, varMatched, 3) {
			t.Fatal()
		}
	}
}

func TestRegexCompileAbnormalVariable(t *testing.T) {
	testData := []string{
		"var1",
		"${var1",
		"$123",
		"var_1$",
		"func1($123, var_3)",
	}

	for _, expr := range testData {
		varMatched := regexCompileVariable.FindStringSubmatch(expr)
		if !assert.Len(t, varMatched, 0) {
			t.Fatal()
		}
	}
}

func TestRegexCompileFunction(t *testing.T) {
	testData := []string{
		"${func1()}",
		"${func1($a)}",
		"${func1($a, $b)}",
		"${func1($a, 123)}",
		"${func1(123, $b)}",
		"abc${func1(123, $b)}123",
	}

	for _, expr := range testData {
		varMatched := regexCompileFunction.FindStringSubmatch(expr)
		if !assert.Len(t, varMatched, 3) {
			t.Fatal()
		}
	}
}

func TestRegexCompileAbnormalFunction(t *testing.T) {
	testData := []string{
		"${func1()",
		"${func1(}",
		"${func1)}",
		"$func1()}",
		"${1func1()}", // function name can not start with number
		"${func1($a}",
		"abc$func1(123, $b)}123",
		// "${func1($a $b)}",
		// "${func1($a, $123)}",
		// "${func1(123 $b)}",
	}

	for _, expr := range testData {
		varMatched := regexCompileFunction.FindStringSubmatch(expr)
		if !assert.Len(t, varMatched, 0) {
			t.Fatal()
		}
	}
}

func TestParseDataStringWithVariables(t *testing.T) {
	variablesMapping := map[string]interface{}{
		"var_1": "abc",
		"var_2": "def",
		"var_3": 123,
		"var_4": map[string]interface{}{"a": 1},
		"var_5": true,
		"var_6": nil,
		"v":     4.5, // variable name with one character
		"_v":    6.9, // variable name starts with underscore
	}

	testData := []struct {
		expr   string
		expect interface{}
	}{
		// no variable
		{"var_1", "var_1"},
		// single variable
		{"$var_1", "abc"},
		{"${var_1}", "abc"},
		{"$var_3", 123},
		{"$var_4", map[string]interface{}{"a": 1}},
		{"${var_4}", map[string]interface{}{"a": 1}},
		{"$var_5", true},
		{"$var_6", nil},
		{"$v", 4.5},
		{"var_1$_v", "var_16.9"},
		// single variable with prefix or suffix
		{"$var_1#XYZ", "abc#XYZ"},
		{"${var_1}#XYZ", "abc#XYZ"},
		{"ABC$var_1", "ABCabc"},
		{"ABC${var_1}", "ABCabc"},
		{"ABC$var_1/", "ABCabc/"},
		{"ABC${v}/", "ABC4.5/"},
		// multiple variables
		{"/$var_1/$var_2/var3", "/abc/def/var3"},
		{"/${var_1}/$var_2/var3", "/abc/def/var3"},
		{"ABC$var_1$var_3", "ABCabc123"},
		{"ABC$var_1${var_3}", "ABCabc123"},
		{"ABC$var_1/$var_3", "ABCabc/123"},
		{"ABC${var_1}/${var_3}", "ABCabc/123"},
		{"ABC$var_1/123$var_1/456", "ABCabc/123abc/456"},
		{"ABC$var_1/123${var_1}/456", "ABCabc/123abc/456"},
		{"ABC$var_1/$var_2/$var_1", "ABCabc/def/abc"},
		{"ABC$var_1/$var_2/${var_1}", "ABCabc/def/abc"},
		{"func1($var_1, $var_3)", "func1(abc, 123)"},
		{"func1($var_1, ${var_3})", "func1(abc, 123)"},
		// TODO: fix compatibility with python version
		{"abc$var_4", "abcmap[a:1]"}, // "abc{'a': 1}"
		{"abc$var_5", "abctrue"},     // "abcTrue"
	}

	parser := newParser()
	for _, data := range testData {
		parsedData, err := parser.Parse(data.expr, variablesMapping)
		if !assert.NoError(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expect, parsedData) {
			t.Fatal()
		}
	}
}

func TestParseDataStringWithUndefinedVariables(t *testing.T) {
	variablesMapping := map[string]interface{}{
		"var_1": "abc",
		"var_2": "def",
	}

	testData := []struct {
		expr   string
		expect interface{}
	}{
		{"/api/$SECRET_KEY", "/api/$SECRET_KEY"}, // raise error
	}

	parser := newParser()
	for _, data := range testData {
		parsedData, err := parser.Parse(data.expr, variablesMapping)
		if !assert.Error(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expect, parsedData) {
			t.Fatal()
		}
	}
}

func TestParseDataStringWithVariablesAbnormal(t *testing.T) {
	variablesMapping := map[string]interface{}{
		"var_1": "abc",
		"var_2": "def",
		"var_3": 123,
		"var_4": map[string]interface{}{"a": 1},
		"var_5": true,
		"var_6": nil,
		"v":     4.5, // variable name with one character
		"_v":    6.9, // variable name starts with underscore
	}

	testData := []struct {
		expr   string
		expect interface{}
	}{
		{"$", "$"},
		{"var_1$", "var_1$"},
		{"var_1$123", "var_1$123"},        // variable should starts with a letter
		{"ABC$var_1{", "ABCabc{"},         // {
		{"ABC$var_1}", "ABCabc}"},         // }
		{"{ABC$var_1{}a}", "{ABCabc{}a}"}, // {xx}
		{"AB{C$var_1{}a}", "AB{Cabc{}a}"}, // {xx{}x}
		{"ABC$$var_1{", "ABC$var_1{"},     // $$
		{"ABC$$$var_1{", "ABC$abc{"},      // $$$
		{"ABC$$$$var_1{", "ABC$$var_1{"},  // $$$$
		{"ABC$var_1${", "ABCabc${"},       // ${
		{"ABC$var_1${a", "ABCabc${a"},     // ${
		{"ABC$var_1$}a", "ABCabc$}a"},     // $}
		{"ABC$var_1}{a", "ABCabc}{a"},     // }{
		{"ABC$var_1{}a", "ABCabc{}a"},     // {}
	}

	parser := newParser()
	for _, data := range testData {
		parsedData, err := parser.Parse(data.expr, variablesMapping)
		if !assert.NoError(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expect, parsedData) {
			t.Fatal()
		}
	}
}

func TestParseDataMapWithVariables(t *testing.T) {
	variablesMapping := map[string]interface{}{
		"var1": "foo1",
		"val1": 200,
		"var2": 123, // key is int
	}

	testData := []struct {
		expr   map[string]interface{}
		expect interface{}
	}{
		{map[string]interface{}{"key": "$var1"}, map[string]interface{}{"key": "foo1"}},
		{map[string]interface{}{"foo1": "$val1", "foo2": "bar2"}, map[string]interface{}{"foo1": 200, "foo2": "bar2"}},
		// parse map key, key is string
		{map[string]interface{}{"$var1": "$val1"}, map[string]interface{}{"foo1": 200}},
		// parse map key, key is int
		{map[string]interface{}{"$var2": "$val1"}, map[string]interface{}{"123": 200}},
	}

	parser := newParser()
	for _, data := range testData {
		parsedData, err := parser.Parse(data.expr, variablesMapping)
		if !assert.NoError(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expect, parsedData) {
			t.Fatal()
		}
	}
}

func TestParseHeaders(t *testing.T) {
	variablesMapping := map[string]interface{}{
		"var1": "foo1",
		"val1": 200,
		"var2": 123, // key is int
		"val2": nil, // value is nil
	}

	testData := []struct {
		rawHeaders    map[string]string
		expectHeaders map[string]string
	}{
		{map[string]string{"key": "$var1"}, map[string]string{"key": "foo1"}},
		{map[string]string{"foo1": "$val1", "foo2": "bar2"}, map[string]string{"foo1": "200", "foo2": "bar2"}},
		// parse map key, key is string
		{map[string]string{"$var1": "$val1"}, map[string]string{"foo1": "200"}},
		// parse map key, key is int
		{map[string]string{"$var2": "$val1"}, map[string]string{"123": "200"}},
		// parse map key & value, key is int, value is nil
		{map[string]string{"$var2": "$val2"}, map[string]string{"123": "<nil>"}},
	}

	parser := newParser()
	for _, data := range testData {
		parsedHeaders, err := parser.ParseHeaders(data.rawHeaders, variablesMapping)
		if !assert.NoError(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expectHeaders, parsedHeaders) {
			t.Fatal()
		}
	}
}

func TestMergeVariables(t *testing.T) {
	testData := []struct {
		stepVariables   map[string]interface{}
		configVariables map[string]interface{}
		expectVariables map[string]interface{}
	}{
		{
			map[string]interface{}{"base_url": "$base_url", "foo1": "bar1"},
			map[string]interface{}{"base_url": "https://httpbin.org", "foo1": "bar111"},
			map[string]interface{}{"base_url": "https://httpbin.org", "foo1": "bar1"},
		},
		{
			map[string]interface{}{"n": 3, "b": 34.5, "varFoo2": "${max($a, $b)}"},
			map[string]interface{}{"n": 5, "a": 12.3, "b": 3.45, "varFoo1": "7a6K3", "varFoo2": 12.3},
			map[string]interface{}{"n": 3, "a": 12.3, "b": 34.5, "varFoo1": "7a6K3", "varFoo2": "${max($a, $b)}"},
		},
	}

	for _, data := range testData {
		mergedVariables := mergeVariables(data.stepVariables, data.configVariables)
		if !assert.Equal(t, data.expectVariables, mergedVariables) {
			t.Fatal()
		}
	}
}

func TestMergeMap(t *testing.T) {
	testData := []struct {
		m             map[string]string
		overriddenMap map[string]string
		expectMap     map[string]string
	}{
		{
			map[string]string{"Accept": "*/*", "Accept-Encoding": "gzip, deflate, br", "Connection": "close"},
			map[string]string{"Cache-Control": "no-cache", "Connection": "keep-alive"},
			map[string]string{"Accept": "*/*", "Accept-Encoding": "gzip, deflate, br", "Connection": "close", "Cache-Control": "no-cache"},
		},
		{
			map[string]string{"Host": "postman-echo.com", "Postman-Token": "ea19464c-ddd4-4724-abe9-5e2b254c2723"},
			map[string]string{"Host": "Postman-echo.com", "Connection": "keep-alive", "Postman-Token": "ea19464c-ddd4-4724-abe9-5e2b342c2723"},
			map[string]string{"Host": "postman-echo.com", "Postman-Token": "ea19464c-ddd4-4724-abe9-5e2b254c2723", "Connection": "keep-alive"},
		},
		{
			map[string]string{"Accept": "*/*", "Accept-Encoding": "gzip, deflate, br", "Connection": "close"},
			nil,
			map[string]string{"Accept": "*/*", "Accept-Encoding": "gzip, deflate, br", "Connection": "close"},
		},
		{
			nil,
			map[string]string{"Cache-Control": "no-cache", "Connection": "keep-alive"},
			map[string]string{"Cache-Control": "no-cache", "Connection": "keep-alive"},
		},
	}

	for _, data := range testData {
		mergedMap := mergeMap(data.m, data.overriddenMap)
		if !assert.Equal(t, data.expectMap, mergedMap) {
			t.Fatal()
		}
	}
}

func TestMergeSlices(t *testing.T) {
	testData := []struct {
		slice           []string
		overriddenSlice []string
		expectSlice     []string
	}{
		{
			[]string{"${setup_hook_example1($name)}", "${setup_hook_example2($name)}"},
			[]string{"${setup_hook_example3($name)}", "${setup_hook_example4($name)}"},
			[]string{"${setup_hook_example1($name)}", "${setup_hook_example2($name)}", "${setup_hook_example3($name)}", "${setup_hook_example4($name)}"},
		},
		{
			[]string{"${setup_hook_example1($name)}", "${setup_hook_example2($name)}"},
			nil,
			[]string{"${setup_hook_example1($name)}", "${setup_hook_example2($name)}"},
		},
		{
			nil,
			[]string{"${setup_hook_example3($name)}", "${setup_hook_example4($name)}"},
			[]string{"${setup_hook_example3($name)}", "${setup_hook_example4($name)}"},
		},
	}

	for _, data := range testData {
		mergedSlice := mergeSlices(data.slice, data.overriddenSlice)
		if !assert.Equal(t, data.expectSlice, mergedSlice) {
			t.Fatal()
		}
	}
}

func TestMergeValidators(t *testing.T) {
	testData := []struct {
		validators           []interface{}
		overriddenValidators []interface{}
		expectValidators     []interface{}
	}{
		{
			[]interface{}{Validator{Check: "status_code", Assert: "equals", Expect: 200, Message: "assert response status code"}},
			[]interface{}{Validator{Check: `headers."Content-Type"`, Assert: "equals", Expect: "application/json; charset=utf-8", Message: "assert response header Content-Typ"}},
			[]interface{}{
				Validator{Check: "status_code", Assert: "equals", Expect: 200, Message: "assert response status code"},
				Validator{Check: `headers."Content-Type"`, Assert: "equals", Expect: "application/json; charset=utf-8", Message: "assert response header Content-Typ"},
			},
		},
		{
			[]interface{}{Validator{Check: "status_code", Assert: "equals", Expect: 302, Message: "assert response status code"}},
			[]interface{}{Validator{Check: "status_code", Assert: "equals", Expect: 200, Message: "assert response status code"}},
			[]interface{}{Validator{Check: "status_code", Assert: "equals", Expect: 302, Message: "assert response status code"}},
		},
		{
			nil,
			[]interface{}{Validator{Check: "status_code", Assert: "equals", Expect: 200, Message: "assert response status code"}},
			[]interface{}{Validator{Check: "status_code", Assert: "equals", Expect: 200, Message: "assert response status code"}},
		},
		{
			[]interface{}{Validator{Check: "status_code", Assert: "equals", Expect: 302, Message: "assert response status code"}},
			nil,
			[]interface{}{Validator{Check: "status_code", Assert: "equals", Expect: 302, Message: "assert response status code"}},
		},
	}

	for _, data := range testData {
		mergedValidators := mergeValidators(data.validators, data.overriddenValidators)
		if !assert.Equal(t, data.expectValidators, mergedValidators) {
			t.Fatal()
		}
	}
}

func TestCallBuiltinFunction(t *testing.T) {
	parser := newParser()

	// call function without arguments
	_, err := parser.callFunc("get_timestamp")
	if !assert.NoError(t, err) {
		t.Fatal()
	}

	// call function with one argument
	timeStart := time.Now()
	_, err = parser.callFunc("sleep", 1)
	if !assert.NoError(t, err) {
		t.Fatal()
	}
	if !assert.Greater(t, time.Since(timeStart), time.Duration(1)*time.Second) {
		t.Fatal()
	}

	// call function with one argument
	result, err := parser.callFunc("gen_random_string", 10)
	if !assert.NoError(t, err) {
		t.Fatal()
	}
	if !assert.Equal(t, 10, len(result.(string))) {
		t.Fatal()
	}

	// call function with two argument
	result, err = parser.callFunc("max", float64(10), 9.99)
	if !assert.NoError(t, err) {
		t.Fatal()
	}
	if !assert.Equal(t, float64(10), result.(float64)) {
		t.Fatal()
	}
}

func TestLiteralEval(t *testing.T) {
	testData := []struct {
		expr   string
		expect interface{}
	}{
		{"123", 123},
		{"1.23", 1.23},
		{"-123", -123},
		{"-1.23", -1.23},
		{"abc", "abc"},
		{" a bc ", "a bc"},
		{" a $bc ", "a $bc"},
		{"$var", "$var"},
		{" $var ", "$var"},
		{" $var1 ", "$var1"},
		{"", ""},
	}

	for _, data := range testData {
		value, err := literalEval(data.expr)
		if !assert.NoError(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expect, value) {
			t.Fatal()
		}
	}
}

func TestParseFunctionArguments(t *testing.T) {
	testData := []struct {
		expr   string
		expect interface{}
	}{
		{"", []interface{}{}},
		{"123", []interface{}{123}},
		{"1.23", []interface{}{1.23}},
		{"-123", []interface{}{-123}},
		{"-1.23", []interface{}{-1.23}},
		{"abc", []interface{}{"abc"}},
		{"$var", []interface{}{"$var"}},
		{"1,2", []interface{}{1, 2}},
		{"1,2.3", []interface{}{1, 2.3}},
		{"1, -2.3", []interface{}{1, -2.3}},
		{"1,,2", []interface{}{1, nil, 2}},
		{" $var1 , 2 ", []interface{}{"$var1", 2}},
	}

	for _, data := range testData {
		value, err := parseFunctionArguments(data.expr)
		if !assert.NoError(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expect, value) {
			t.Fatal()
		}
	}
}

func TestParseDataStringWithFunctions(t *testing.T) {
	variablesMapping := map[string]interface{}{
		"n": 5,
		"a": 12.3,
		"b": 3.45,
	}

	testData1 := []struct {
		expr   string
		expect interface{}
	}{
		{"${gen_random_string(5)}", 5},
		{"${gen_random_string($n)}", 5},
		{"123${gen_random_string(5)}abc", 11},
		{"123${gen_random_string($n)}abc", 11},
	}

	parser := newParser()
	for _, data := range testData1 {
		value, err := parser.Parse(data.expr, variablesMapping)
		if !assert.NoError(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expect, len(value.(string))) {
			t.Fatal()
		}
	}

	testData2 := []struct {
		expr   string
		expect interface{}
	}{
		{"${max($a, $b)}", 12.3},
		{"abc${max($a, $b)}123", "abc12.3123"},
		{"abc${max($a, 3.45)}123", "abc12.3123"},
	}

	for _, data := range testData2 {
		value, err := parser.Parse(data.expr, variablesMapping)
		if !assert.NoError(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expect, value) {
			t.Fatal()
		}
	}
}

func TestConvertString(t *testing.T) {
	testData := []struct {
		raw    interface{}
		expect interface{}
	}{
		{"", ""},
		{"abc", "abc"},
		{"123", "123"},
		{123, "123"},
		{1.23, "1.23"},
		{100000000000, "100000000000"}, // avoid exponential notation
		{100000000000.23, "100000000000.23"},
		{nil, "<nil>"},
	}

	for _, data := range testData {
		value := convertString(data.raw)
		if !assert.Equal(t, data.expect, value) {
			t.Fatal()
		}
	}
}

func TestParseVariables(t *testing.T) {
	testData := []struct {
		rawVars    map[string]interface{}
		expectVars map[string]interface{}
	}{
		{
			map[string]interface{}{"varA": "$varB", "varB": "$varC", "varC": "123", "a": 1, "b": 2},
			map[string]interface{}{"varA": "123", "varB": "123", "varC": "123", "a": int64(1), "b": int64(2)},
		},
		{
			map[string]interface{}{"n": 34.5, "a": 12.3, "b": "$n", "varFoo2": "${max($a, $b)}"},
			map[string]interface{}{"n": 34.5, "a": 12.3, "b": 34.5, "varFoo2": 34.5},
		},
	}

	parser := newParser()
	for _, data := range testData {
		value, err := parser.ParseVariables(data.rawVars)
		if !assert.NoError(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expectVars, value) {
			t.Fatal()
		}
	}
}

func TestParseVariablesAbnormal(t *testing.T) {
	testData := []struct {
		rawVars    map[string]interface{}
		expectVars map[string]interface{}
	}{
		{ // self referenced variable $varA
			map[string]interface{}{"varA": "$varA"},
			map[string]interface{}{"varA": "$varA"},
		},
		{ // undefined variable $varC
			map[string]interface{}{"varA": "$varB", "varB": "$varC", "a": 1, "b": 2},
			map[string]interface{}{"varA": "$varB", "varB": "$varC", "a": 1, "b": 2},
		},
		{ // circular reference
			map[string]interface{}{"varA": "$varB", "varB": "$varA"},
			map[string]interface{}{"varA": "$varB", "varB": "$varA"},
		},
	}

	parser := newParser()
	for _, data := range testData {
		value, err := parser.ParseVariables(data.rawVars)
		if !assert.Error(t, err) {
			t.Fatal()
		}
		if !assert.Equal(t, data.expectVars, value) {
			t.Fatal()
		}
	}
}

func TestExtractVariables(t *testing.T) {
	testData := []struct {
		raw        interface{}
		expectVars []string
	}{
		{nil, nil},
		{"/$var1/$var1", []string{"var1"}},
		{
			map[string]interface{}{"varA": "$varB", "varB": "$varC", "varC": "123"},
			[]string{"varB", "varC"},
		},
		{
			[]interface{}{"varA", "$varB", 123, "$varC", "123"},
			[]string{"varB", "varC"},
		},
		{ // nested map and slice
			map[string]interface{}{"varA": "$varB", "varB": map[string]interface{}{"C": "$varC", "D": []string{"$varE"}}},
			[]string{"varB", "varC", "varE"},
		},
	}

	for _, data := range testData {
		var varList []string
		for varName := range extractVariables(data.raw) {
			varList = append(varList, varName)
		}
		sort.Strings(varList)
		if !assert.Equal(t, data.expectVars, varList) {
			t.Fatal()
		}
	}
}

func TestFindallVariables(t *testing.T) {
	testData := []struct {
		raw        string
		expectVars []string
	}{
		{"", nil},
		{"$variable", []string{"variable"}},
		{"${variable}123", []string{"variable"}},
		{"/blog/$postid", []string{"postid"}},
		{"/$var1/$var2", []string{"var1", "var2"}},
		{"/$var1/$var1", []string{"var1"}},
		{"abc", nil},
		{"Z:2>1*0*1+1$a", []string{"a"}},
		{"Z:2>1*0*1+1$$a", nil},
		{"Z:2>1*0*1+1$$$a", []string{"a"}},
		{"Z:2>1*0*1+1$$$$a", nil},
		{"Z:2>1*0*1+1$$a$b", []string{"b"}},
		{"Z:2>1*0*1+1$$a$$b", nil},
		{"Z:2>1*0*1+1$a$b", []string{"a", "b"}},
		{"Z:2>1*0*1+1$$1", nil},
		{"a$var", []string{"var"}},
		{"a$v b", []string{"v"}},
		{"${func()}", nil},
		{"a${func(1,2)}b", nil},
		{"${gen_md5($TOKEN, $data, $random)}", []string{"TOKEN", "data", "random"}},
	}

	for _, data := range testData {
		var varList []string
		for varName := range findallVariables(data.raw) {
			varList = append(varList, varName)
		}
		sort.Strings(varList)
		if !assert.Equal(t, data.expectVars, varList) {
			t.Fatal()
		}
	}
}
