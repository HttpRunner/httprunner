from typing import Dict, Text, Any

import jmespath
from jmespath.exceptions import JMESPathError
from loguru import logger

from httprunner import exceptions
from httprunner.exceptions import ValidationFailure, ParamsError
from httprunner.models import VariablesMapping, Validators
from httprunner.parser import parse_string_value, Parser


def get_uniform_comparator(comparator: Text):
    """convert comparator alias to uniform name"""
    if comparator in ["eq", "equals", "equal"]:
        return "equal"
    elif comparator in ["lt", "less_than"]:
        return "less_than"
    elif comparator in ["le", "less_or_equals"]:
        return "less_or_equals"
    elif comparator in ["gt", "greater_than"]:
        return "greater_than"
    elif comparator in ["ge", "greater_or_equals"]:
        return "greater_or_equals"
    elif comparator in ["ne", "not_equal"]:
        return "not_equal"
    elif comparator in ["str_eq", "string_equals"]:
        return "string_equals"
    elif comparator in ["len_eq", "length_equal"]:
        return "length_equal"
    elif comparator in [
        "len_gt",
        "length_greater_than",
    ]:
        return "length_greater_than"
    elif comparator in [
        "len_ge",
        "length_greater_or_equals",
    ]:
        return "length_greater_or_equals"
    elif comparator in ["len_lt", "length_less_than"]:
        return "length_less_than"
    elif comparator in [
        "len_le",
        "length_less_or_equals",
    ]:
        return "length_less_or_equals"
    elif comparator in ["contains", "contained_by", "type_match", "regex_match", "startswith", "endswith"]:
        return comparator
    else:
        return f"custom_{comparator}"



def uniform_validator(validator):
    """unify validator

    Args:
        validator (dict): validator maybe in two formats:

            format1: this is kept for compatibility with the previous versions.
                {"check": "status_code", "comparator": "eq", "expect": 201, "message": "test"}
                {"check": "status_code", "assert": "eq", "expect": 201, "msg": "test"}
            format2: recommended new version, {assert: [check_item, expected_value, msg]}
                {'eq': ['status_code', 201, "test"]}

    Returns
        dict: validator info

            {
                "check": "status_code",
                "expect": 201,
                "assert": "equal",
                "message": "test
            }

    """
    if not isinstance(validator, dict):
        raise ParamsError(f"invalid validator: {validator}")

    if "check" in validator and "expect" in validator:
        # format1
        check_item = validator["check"]
        expect_value = validator["expect"]

        if "assert" in validator:
            comparator = validator.get("assert")
        else:
            comparator = validator.get("comparator", "eq")

        if "msg" in validator:
            message = validator.get("msg")
        else:
            message = validator.get("message", "")

    elif len(validator) == 1:
        # format2
        comparator = list(validator.keys())[0]
        compare_values = validator[comparator]

        if not isinstance(compare_values, list) or len(compare_values) not in [2, 3]:
            raise ParamsError(f"invalid validator: {validator}")

        check_item = compare_values[0]
        expect_value = compare_values[1]
        if len(compare_values) == 3:
            message = compare_values[2]
        else:
            # len(compare_values) == 2
            message = ""

    else:
        raise ParamsError(f"invalid validator: {validator}")

    # uniform comparator, e.g. lt => less_than, eq => equals
    assert_method = get_uniform_comparator(comparator)

    return {
        "check": check_item,
        "expect": expect_value,
        "assert": assert_method,
        "message": message,
    }


class ResponseObjectBase(object):
    def __init__(self, resp_obj, parser: Parser):
        """initialize with a response object

        Args:
            resp_obj (instance): requests.Response instance

        """
        self.resp_obj = resp_obj
        self.parser = parser
        self.validation_results: Dict = {}

    def extract(
        self,
        extractors: Dict[Text, Text],
        variables_mapping: VariablesMapping = None,
    ) -> Dict[Text, Any]:
        if not extractors:
            return {}

        extract_mapping = {}
        for key, field in extractors.items():
            if "$" in field:
                # field contains variable or function
                field = self.parser.parse_data(field, variables_mapping)
            field_value = self._search_jmespath(field)
            extract_mapping[key] = field_value

        logger.info(f"extract mapping: {extract_mapping}")
        return extract_mapping

    def _search_jmespath(self, expr: Text) -> Any:
        try:
            check_value = jmespath.search(expr, self.resp_obj)
        except JMESPathError as ex:
            logger.error(
                f"failed to search with jmespath\n"
                f"expression: {expr}\n"
                f"data: {self.resp_obj}\n"
                f"exception: {ex}"
            )
            raise
        return check_value

    def validate(
        self,
        validators: Validators,
        variables_mapping: VariablesMapping = None,
    ):

        variables_mapping = variables_mapping or {}

        self.validation_results = {}
        if not validators:
            return

        validate_pass = True
        failures = []

        for v in validators:

            if "validate_extractor" not in self.validation_results:
                self.validation_results["validate_extractor"] = []

            u_validator = uniform_validator(v)

            # check item
            check_item = u_validator["check"]
            if "$" in check_item:
                # check_item is variable or function
                check_item = self.parser.parse_data(check_item, variables_mapping)
                check_item = parse_string_value(check_item)

            if check_item and isinstance(check_item, Text):
                check_value = self._search_jmespath(check_item)
            else:
                # variable or function evaluation result is "" or not text
                check_value = check_item

            # comparator
            assert_method = u_validator["assert"]
            if assert_method.startswith("custom"):
                assert_method = assert_method.split("_", 1)[1]
            assert_func = self.parser.get_mapping_function(assert_method)

            # expect item
            expect_item = u_validator["expect"]
            # parse expected value with config/teststep/extracted variables
            expect_value = self.parser.parse_data(expect_item, variables_mapping)

            # message
            message = u_validator["message"]
            # parse message with config/teststep/extracted variables
            message = self.parser.parse_data(message, variables_mapping)

            validate_msg = f"assert {check_item} {assert_method} {expect_value}({type(expect_value).__name__})"

            validator_dict = {
                "comparator": assert_method,
                "check": check_item,
                "check_value": check_value,
                "expect": expect_item,
                "expect_value": expect_value,
                "message": message,
            }

            try:
                assert_func(check_value, expect_value, message)
                validate_msg += "\t==> pass"
                logger.info(validate_msg)
                validator_dict["check_result"] = "pass"
            except AssertionError as ex:
                validate_pass = False
                validator_dict["check_result"] = "fail"
                validate_msg += "\t==> fail"
                validate_msg += (
                    f"\n"
                    f"check_item: {check_item}\n"
                    f"check_value: {check_value}({type(check_value).__name__})\n"
                    f"assert_method: {assert_method}\n"
                    f"expect_value: {expect_value}({type(expect_value).__name__})"
                )
                message = str(ex)
                if message:
                    validate_msg += f"\nmessage: {message}"

                logger.error(validate_msg)
                failures.append(validate_msg)

            self.validation_results["validate_extractor"].append(validator_dict)

        if not validate_pass:
            failures_string = "\n".join([failure for failure in failures])
            raise ValidationFailure(failures_string)


class ResponseObject(ResponseObjectBase):
    def __getattr__(self, key):
        if key in ["json", "content", "body"]:
            try:
                value = self.resp_obj.json()
            except ValueError:
                value = self.resp_obj.content
        elif key == "cookies":
            value = self.resp_obj.cookies.get_dict()
        else:
            try:
                value = getattr(self.resp_obj, key)
            except AttributeError:
                err_msg = "ResponseObject does not have attribute: {}".format(key)
                logger.error(err_msg)
                raise exceptions.ParamsError(err_msg)

        self.__dict__[key] = value
        return value

    def _search_jmespath(self, expr: Text) -> Any:
        resp_obj_meta = {
            "status_code": self.status_code,
            "headers": self.headers,
            "cookies": self.cookies,
            "body": self.body,
        }
        if not expr.startswith(tuple(resp_obj_meta.keys())):
            if hasattr(self.resp_obj,expr):
                return getattr(self.resp_obj,expr)
            else:
                return expr

        try:
            check_value = jmespath.search(expr, resp_obj_meta)
        except JMESPathError as ex:
            logger.error(
                f"failed to search with jmespath\n"
                f"expression: {expr}\n"
                f"data: {resp_obj_meta}\n"
                f"exception: {ex}"
            )
            raise

        return check_value


class ThriftResponseObject(ResponseObjectBase):
    pass


class SqlResponseObject(ResponseObjectBase):
    pass
