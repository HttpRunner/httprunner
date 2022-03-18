import os
import time
import unittest

from httprunner import parser, loader
from httprunner.exceptions import VariableNotFound, FunctionNotFound
from httprunner.loader import load_project_meta


class TestParserBasic(unittest.TestCase):
    def test_parse_variables_mapping(self):
        variables = {"varA": "$varB", "varB": "$varC", "varC": "123", "a": 1, "b": 2}
        parsed_variables = parser.parse_variables_mapping(variables)
        print(parsed_variables)
        self.assertEqual(parsed_variables["varA"], "123")
        self.assertEqual(parsed_variables["varB"], "123")

    def test_parse_variables_mapping_exception(self):
        variables = {"varA": "$varB", "varB": "$varC", "a": 1, "b": 2}
        with self.assertRaises(VariableNotFound):
            parser.parse_variables_mapping(variables)

    def test_parse_string_value(self):
        self.assertEqual(parser.parse_string_value("123"), 123)
        self.assertEqual(parser.parse_string_value("12.3"), 12.3)
        self.assertEqual(parser.parse_string_value("a123"), "a123")
        self.assertEqual(parser.parse_string_value("$var"), "$var")
        self.assertEqual(parser.parse_string_value("${func}"), "${func}")

    def test_regex_findall_variables(self):
        self.assertEqual(parser.regex_findall_variables("$variable"), ["variable"])
        self.assertEqual(parser.regex_findall_variables("${variable}123"), ["variable"])
        self.assertEqual(parser.regex_findall_variables("/blog/$postid"), ["postid"])
        self.assertEqual(
            parser.regex_findall_variables("/$var1/$var2"), ["var1", "var2"]
        )
        self.assertEqual(parser.regex_findall_variables("abc"), [])
        self.assertEqual(parser.regex_findall_variables("Z:2>1*0*1+1$a"), ["a"])
        self.assertEqual(parser.regex_findall_variables("Z:2>1*0*1+1$$a"), [])
        self.assertEqual(parser.regex_findall_variables("Z:2>1*0*1+1$$$a"), ["a"])
        self.assertEqual(parser.regex_findall_variables("Z:2>1*0*1+1$$$$a"), [])
        self.assertEqual(parser.regex_findall_variables("Z:2>1*0*1+1$$a$b"), ["b"])
        self.assertEqual(parser.regex_findall_variables("Z:2>1*0*1+1$$a$$b"), [])

    def test_extract_variables(self):
        self.assertEqual(parser.extract_variables("$var"), {"var"})
        self.assertEqual(parser.extract_variables("$var123"), {"var123"})
        self.assertEqual(parser.extract_variables("$var_name"), {"var_name"})
        self.assertEqual(parser.extract_variables("var"), set())
        self.assertEqual(parser.extract_variables("a$var"), {"var"})
        self.assertEqual(parser.extract_variables("$v ar"), {"v"})
        self.assertEqual(parser.extract_variables(" "), set())
        self.assertEqual(parser.extract_variables("$abc*"), {"abc"})
        self.assertEqual(parser.extract_variables("${func()}"), set())
        self.assertEqual(parser.extract_variables("${func(1,2)}"), set())
        self.assertEqual(
            parser.extract_variables("${gen_md5($TOKEN, $data, $random)}"),
            {"TOKEN", "data", "random"},
        )
        self.assertEqual(parser.extract_variables("Z:2>1*0*1+1$$1"), set())

    def test_parse_function_params(self):
        self.assertEqual(parser.parse_function_params(""), {"args": [], "kwargs": {}})
        self.assertEqual(parser.parse_function_params("5"), {"args": [5], "kwargs": {}})
        self.assertEqual(
            parser.parse_function_params("1, 2"), {"args": [1, 2], "kwargs": {}}
        )
        self.assertEqual(
            parser.parse_function_params("a=1, b=2"),
            {"args": [], "kwargs": {"a": 1, "b": 2}},
        )
        self.assertEqual(
            parser.parse_function_params("a= 1, b =2"),
            {"args": [], "kwargs": {"a": 1, "b": 2}},
        )
        self.assertEqual(
            parser.parse_function_params("1, 2, a=3, b=4"),
            {"args": [1, 2], "kwargs": {"a": 3, "b": 4}},
        )
        self.assertEqual(
            parser.parse_function_params("$request, 123"),
            {"args": ["$request", 123], "kwargs": {}},
        )
        self.assertEqual(parser.parse_function_params("  "), {"args": [], "kwargs": {}})
        self.assertEqual(
            parser.parse_function_params("hello world, a=3, b=4"),
            {"args": ["hello world"], "kwargs": {"a": 3, "b": 4}},
        )
        self.assertEqual(
            parser.parse_function_params("$request, 12 3"),
            {"args": ["$request", "12 3"], "kwargs": {}},
        )

    def test_extract_functions(self):
        self.assertEqual(parser.regex_findall_functions("${func()}"), [("func", "")])
        self.assertEqual(parser.regex_findall_functions("${func(5)}"), [("func", "5")])
        self.assertEqual(
            parser.regex_findall_functions("${func(a=1, b=2)}"), [("func", "a=1, b=2")]
        )
        self.assertEqual(
            parser.regex_findall_functions("${func(1, $b, c=$x, d=4)}"),
            [("func", "1, $b, c=$x, d=4")],
        )
        self.assertEqual(
            parser.regex_findall_functions("/api/1000?_t=${get_timestamp()}"),
            [("get_timestamp", "")],
        )
        self.assertEqual(
            parser.regex_findall_functions("/api/${add(1, 2)}"), [("add", "1, 2")]
        )
        self.assertEqual(
            parser.regex_findall_functions("/api/${add(1, 2)}?_t=${get_timestamp()}"),
            [("add", "1, 2"), ("get_timestamp", "")],
        )
        self.assertEqual(
            parser.regex_findall_functions("abc${func(1, 2, a=3, b=4)}def"),
            [("func", "1, 2, a=3, b=4")],
        )

    def test_parse_data_string_with_variables(self):
        variables_mapping = {
            "var_1": "abc",
            "var_2": "def",
            "var_3": 123,
            "var_4": {"a": 1},
            "var_5": True,
            "var_6": None,
        }
        self.assertEqual(parser.parse_data("$var_1", variables_mapping), "abc")
        self.assertEqual(parser.parse_data("${var_1}", variables_mapping), "abc")
        self.assertEqual(parser.parse_data("var_1", variables_mapping), "var_1")
        self.assertEqual(parser.parse_data("$var_1#XYZ", variables_mapping), "abc#XYZ")
        self.assertEqual(
            parser.parse_data("${var_1}#XYZ", variables_mapping), "abc#XYZ"
        )
        self.assertEqual(
            parser.parse_data("/$var_1/$var_2/var3", variables_mapping), "/abc/def/var3"
        )
        self.assertEqual(parser.parse_data("$var_3", variables_mapping), 123)
        self.assertEqual(parser.parse_data("$var_4", variables_mapping), {"a": 1})
        self.assertEqual(parser.parse_data("$var_5", variables_mapping), True)
        self.assertEqual(parser.parse_data("abc$var_5", variables_mapping), "abcTrue")
        self.assertEqual(
            parser.parse_data("abc$var_4", variables_mapping), "abc{'a': 1}"
        )
        self.assertEqual(parser.parse_data("$var_6", variables_mapping), None)

        with self.assertRaises(VariableNotFound):
            parser.parse_data("/api/$SECRET_KEY", variables_mapping)

        self.assertEqual(
            parser.parse_data(["$var_1", "$var_2"], variables_mapping), ["abc", "def"]
        )
        self.assertEqual(
            parser.parse_data({"$var_1": "$var_2"}, variables_mapping), {"abc": "def"}
        )

        # format: $var
        value = parser.parse_data("ABC$var_1", variables_mapping)
        self.assertEqual(value, "ABCabc")

        value = parser.parse_data("ABC$var_1$var_3", variables_mapping)
        self.assertEqual(value, "ABCabc123")

        value = parser.parse_data("ABC$var_1/$var_3", variables_mapping)
        self.assertEqual(value, "ABCabc/123")

        value = parser.parse_data("ABC$var_1/", variables_mapping)
        self.assertEqual(value, "ABCabc/")

        value = parser.parse_data("ABC$var_1$", variables_mapping)
        self.assertEqual(value, "ABCabc$")

        value = parser.parse_data("ABC$var_1/123$var_1/456", variables_mapping)
        self.assertEqual(value, "ABCabc/123abc/456")

        value = parser.parse_data("ABC$var_1/$var_2/$var_1", variables_mapping)
        self.assertEqual(value, "ABCabc/def/abc")

        value = parser.parse_data("func1($var_1, $var_3)", variables_mapping)
        self.assertEqual(value, "func1(abc, 123)")

        # format: ${var}
        value = parser.parse_data("ABC${var_1}", variables_mapping)
        self.assertEqual(value, "ABCabc")

        value = parser.parse_data("ABC${var_1}${var_3}", variables_mapping)
        self.assertEqual(value, "ABCabc123")

        value = parser.parse_data("ABC${var_1}/${var_3}", variables_mapping)
        self.assertEqual(value, "ABCabc/123")

        value = parser.parse_data("ABC${var_1}/", variables_mapping)
        self.assertEqual(value, "ABCabc/")

        value = parser.parse_data("ABC${var_1}123", variables_mapping)
        self.assertEqual(value, "ABCabc123")

        value = parser.parse_data("ABC${var_1}/123${var_1}/456", variables_mapping)
        self.assertEqual(value, "ABCabc/123abc/456")

        value = parser.parse_data("ABC${var_1}/${var_2}/${var_1}", variables_mapping)
        self.assertEqual(value, "ABCabc/def/abc")

        value = parser.parse_data("func1(${var_1}, ${var_3})", variables_mapping)
        self.assertEqual(value, "func1(abc, 123)")

    def test_parse_data_multiple_identical_variables(self):
        variables_mapping = {
            "var_1": "abc",
            "var_2": "def",
        }
        self.assertEqual(
            parser.parse_data("/$var_1/$var_2/$var_1", variables_mapping),
            "/abc/def/abc",
        )

        variables_mapping = {"userid": 100, "data": 1498}
        content = "/users/$userid/training/$data?userId=$userid&data=$data"
        self.assertEqual(
            parser.parse_data(content, variables_mapping),
            "/users/100/training/1498?userId=100&data=1498",
        )

        variables_mapping = {"user": 100, "userid": 1000, "data": 1498}
        content = "/users/$user/$userid/$data?userId=$userid&data=$data"
        self.assertEqual(
            parser.parse_data(content, variables_mapping),
            "/users/100/1000/1498?userId=1000&data=1498",
        )

    def test_parse_data_string_with_functions(self):
        import random, string

        functions_mapping = {
            "gen_random_string": lambda str_len: "".join(
                random.choice(string.ascii_letters + string.digits)
                for _ in range(str_len)
            )
        }
        result = parser.parse_data(
            "${gen_random_string(5)}", functions_mapping=functions_mapping
        )
        self.assertEqual(len(result), 5)

        add_two_nums = lambda a, b=1: a + b
        functions_mapping["add_two_nums"] = add_two_nums
        self.assertEqual(
            parser.parse_data(
                "${add_two_nums(1)}", functions_mapping=functions_mapping
            ),
            2,
        )
        self.assertEqual(
            parser.parse_data(
                "${add_two_nums(1, 2)}", functions_mapping=functions_mapping
            ),
            3,
        )
        self.assertEqual(
            parser.parse_data(
                "/api/${add_two_nums(1, 2)}", functions_mapping=functions_mapping
            ),
            "/api/3",
        )

        with self.assertRaises(FunctionNotFound):
            parser.parse_data("/api/${gen_md5(abc)}")

        variables_mapping = {
            "var_1": "abc",
            "var_2": "def",
            "var_3": 123,
            "var_4": {"a": 1},
            "var_5": True,
            "var_6": None,
        }
        functions_mapping = {"func1": lambda x, y: str(x) + str(y)}

        value = parser.parse_data(
            "${func1($var_1, $var_3)}", variables_mapping, functions_mapping
        )
        self.assertEqual(value, "abc123")

        value = parser.parse_data(
            "ABC${func1($var_1, $var_3)}DE", variables_mapping, functions_mapping
        )
        self.assertEqual(value, "ABCabc123DE")

        value = parser.parse_data(
            "ABC${func1($var_1, $var_3)}$var_5", variables_mapping, functions_mapping
        )
        self.assertEqual(value, "ABCabc123True")

        value = parser.parse_data(
            "ABC${func1($var_1, $var_3)}DE$var_4", variables_mapping, functions_mapping
        )
        self.assertEqual(value, "ABCabc123DE{'a': 1}")

        value = parser.parse_data(
            "ABC$var_5${func1($var_1, $var_3)}", variables_mapping, functions_mapping
        )
        self.assertEqual(value, "ABCTrueabc123")

        value = parser.parse_data(
            "ABC${ord(a)}DEF${len(abcd)}", variables_mapping, functions_mapping
        )
        self.assertEqual(value, "ABC97DEF4")

    def test_parse_data_func_var_duplicate(self):
        variables_mapping = {
            "var_1": "abc",
            "var_2": "def",
            "var_3": 123,
            "var_4": {"a": 1},
            "var_5": True,
            "var_6": None,
        }
        functions_mapping = {"func1": lambda x, y: str(x) + str(y)}
        value = parser.parse_data(
            "ABC${func1($var_1, $var_3)}--${func1($var_1, $var_3)}",
            variables_mapping,
            functions_mapping,
        )
        self.assertEqual(value, "ABCabc123--abc123")

        value = parser.parse_data(
            "ABC${func1($var_1, $var_3)}$var_1", variables_mapping, functions_mapping
        )
        self.assertEqual(value, "ABCabc123abc")

        value = parser.parse_data(
            "ABC${func1($var_1, $var_3)}$var_1--${func1($var_1, $var_3)}$var_1",
            variables_mapping,
            functions_mapping,
        )
        self.assertEqual(value, "ABCabc123abc--abc123abc")

    def test_parse_data_func_abnormal(self):
        variables_mapping = {
            "var_1": "abc",
            "var_2": "def",
            "var_3": 123,
            "var_4": {"a": 1},
            "var_5": True,
            "var_6": None,
        }
        functions_mapping = {"func1": lambda x, y: str(x) + str(y)}

        # {
        value = parser.parse_data("ABC$var_1{", variables_mapping, functions_mapping)
        self.assertEqual(value, "ABCabc{")

        value = parser.parse_data(
            "{ABC$var_1{}a}", variables_mapping, functions_mapping
        )
        self.assertEqual(value, "{ABCabc{}a}")

        value = parser.parse_data(
            "AB{C$var_1{}a}", variables_mapping, functions_mapping
        )
        self.assertEqual(value, "AB{Cabc{}a}")

        # }
        value = parser.parse_data("ABC$var_1}", variables_mapping, functions_mapping)
        self.assertEqual(value, "ABCabc}")

        # $$
        value = parser.parse_data("ABC$$var_1{", variables_mapping, functions_mapping)
        self.assertEqual(value, "ABC$var_1{")

        # $$$
        value = parser.parse_data("ABC$$$var_1{", variables_mapping, functions_mapping)
        self.assertEqual(value, "ABC$abc{")

        # $$$$
        value = parser.parse_data("ABC$$$$var_1{", variables_mapping, functions_mapping)
        self.assertEqual(value, "ABC$$var_1{")

        # ${
        value = parser.parse_data("ABC$var_1${", variables_mapping, functions_mapping)
        self.assertEqual(value, "ABCabc${")

        value = parser.parse_data("ABC$var_1${a", variables_mapping, functions_mapping)
        self.assertEqual(value, "ABCabc${a")

        # $}
        value = parser.parse_data("ABC$var_1$}a", variables_mapping, functions_mapping)
        self.assertEqual(value, "ABCabc$}a")

        # }{
        value = parser.parse_data("ABC$var_1}{a", variables_mapping, functions_mapping)
        self.assertEqual(value, "ABCabc}{a")

        # {}
        value = parser.parse_data("ABC$var_1{}a", variables_mapping, functions_mapping)
        self.assertEqual(value, "ABCabc{}a")

    def test_parse_data_request(self):
        content = {
            "request": {
                "url": "/api/users/$uid",
                "method": "$method",
                "headers": {"token": "$token"},
                "data": {
                    "null": None,
                    "true": True,
                    "false": False,
                    "empty_str": "",
                    "value": "abc${add_one(3)}def",
                },
            }
        }
        variables_mapping = {"uid": 1000, "method": "POST", "token": "abc123"}
        functions_mapping = {"add_one": lambda x: x + 1}
        result = parser.parse_data(content, variables_mapping, functions_mapping)
        self.assertEqual("/api/users/1000", result["request"]["url"])
        self.assertEqual("abc123", result["request"]["headers"]["token"])
        self.assertEqual("POST", result["request"]["method"])
        self.assertIsNone(result["request"]["data"]["null"])
        self.assertTrue(result["request"]["data"]["true"])
        self.assertFalse(result["request"]["data"]["false"])
        self.assertEqual("", result["request"]["data"]["empty_str"])
        self.assertEqual("abc4def", result["request"]["data"]["value"])

    def test_parse_data_testcase(self):
        variables = {
            "uid": "1000",
            "random": "A2dEx",
            "authorization": "a83de0ff8d2e896dbd8efb81ba14e17d",
            "data": {"name": "user", "password": "123456"},
        }
        functions = {
            "add_two_nums": lambda a, b=1: a + b,
            "get_timestamp": lambda: int(time.time() * 1000),
        }
        testcase_template = {
            "url": "http://127.0.0.1:5000/api/users/$uid/${add_two_nums(1,2)}",
            "method": "POST",
            "headers": {
                "Content-Type": "application/json",
                "authorization": "$authorization",
                "random": "$random",
                "sum": "${add_two_nums(1, 2)}",
            },
            "body": "$data",
        }
        parsed_testcase = parser.parse_data(testcase_template, variables, functions)
        self.assertEqual(
            parsed_testcase["url"], "http://127.0.0.1:5000/api/users/1000/3"
        )
        self.assertEqual(
            parsed_testcase["headers"]["authorization"], variables["authorization"]
        )
        self.assertEqual(parsed_testcase["headers"]["random"], variables["random"])
        self.assertEqual(parsed_testcase["body"], variables["data"])
        self.assertEqual(parsed_testcase["headers"]["sum"], 3)

    def test_parse_parameters_testcase(self):
        parameters = {
            "user_agent": ["iOS/10.1", "iOS/10.2"],
            "username-password": "${parameterize(request_methods/account.csv)}",
            "sum": "${calculate_two_nums(1, 2)}",
        }

        loader.project_meta = None
        load_project_meta(
            os.path.join(
                os.path.dirname(os.path.dirname(__file__)),
                "examples",
                "postman_echo",
                "request_methods",
            ),
        )
        parsed_params = parser.parse_parameters(parameters)
        self.assertEqual(len(parsed_params), 2 * 3 * 2)

        self.assertIn(
            {
                "username": "test1",
                "password": "111111",
                "user_agent": "iOS/10.1",
                "sum": 3,
            },
            parsed_params,
        )
        self.assertIn(
            {
                "username": "test1",
                "password": "111111",
                "user_agent": "iOS/10.1",
                "sum": 1,
            },
            parsed_params,
        )
        self.assertIn(
            {
                "username": "test1",
                "password": "111111",
                "user_agent": "iOS/10.2",
                "sum": 3,
            },
            parsed_params,
        )
        self.assertIn(
            {
                "username": "test1",
                "password": "111111",
                "user_agent": "iOS/10.2",
                "sum": 1,
            },
            parsed_params,
        )
        self.assertIn(
            {
                "username": "test2",
                "password": "222222",
                "user_agent": "iOS/10.1",
                "sum": 3,
            },
            parsed_params,
        )
        self.assertIn(
            {
                "username": "test2",
                "password": "222222",
                "user_agent": "iOS/10.1",
                "sum": 1,
            },
            parsed_params,
        )
        self.assertIn(
            {
                "username": "test2",
                "password": "222222",
                "user_agent": "iOS/10.2",
                "sum": 3,
            },
            parsed_params,
        )
        self.assertIn(
            {
                "username": "test2",
                "password": "222222",
                "user_agent": "iOS/10.2",
                "sum": 1,
            },
            parsed_params,
        )
