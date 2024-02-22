from typing import Dict, Text, Any

import jmespath
import requests
from jmespath.exceptions import JMESPathError
from loguru import logger

from httprunner import exceptions
from httprunner.exceptions import ValidationFailure, ParamsError
from httprunner.models import VariablesMapping, Validators, FunctionsMapping
from httprunner.parser import parse_data, parse_string_value, get_mapping_function


def get_uniform_comparator(comparator: Text):
    """ convert comparator alias to uniform name
    """
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
    else:
        return comparator


def uniform_validator(validator):
    """ unify validator

    Args:
        validator (dict): validator maybe in two formats:

            format1: this is kept for compatibility with the previous versions.
                {"check": "status_code", "comparator": "eq", "expect": 201}
                {"check": "$resp_body_success", "comparator": "eq", "expect": True}
            format2: recommended new version, {assert: [check_item, expected_value]}
                {'eq': ['status_code', 201]}
                {'eq': ['$resp_body_success', True]}

    Returns
        dict: validator info

            {
                "check": "status_code",
                "expect": 201,
                "assert": "equals"
            }

    """
    if not isinstance(validator, dict):
        raise ParamsError(f"invalid validator: {validator}")

    if "check" in validator and "expect" in validator:
        # format1
        check_item = validator["check"]
        expect_value = validator["expect"]
        message = validator.get("message", "")
        comparator = validator.get("comparator", "eq")

    elif len(validator) == 1:
        # format2
        comparator = list(validator.keys())[0]
        compare_values = validator[comparator]
        if comparator == "custom":
            custom_comparator = compare_values[0]
            check_item = compare_values[1]
            expect_value = compare_values[2]
            if len(compare_values) == 4:
                message = compare_values[3]
            else:
                # len(compare_values) == 2
                message = ""
        else:
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
    if assert_method == "custom":
        return {
            "check": check_item,
            "expect": expect_value,
            "assert": assert_method,
            "message": message,
            "custom_comparator": custom_comparator,
        }
    else:
        return {
            "check": check_item,
            "expect": expect_value,
            "assert": assert_method,
            "message": message,
        }


class ResponseObject(object):
    def __init__(self, resp_obj: requests.Response):
        """ initialize with a requests.Response object

        Args:
            resp_obj (instance): requests.Response instance

        """
        self.resp_obj = resp_obj
        self.validation_results: Dict = {}

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

    def extract(self,
                extractors: Dict[Text, Text],
                variables_mapping: VariablesMapping = None,
                functions_mapping: FunctionsMapping = None,
                ) -> Dict[Text, Any]:
        if not extractors:
            return {}

        extract_mapping = {}
        for key, field in extractors.items():
            if '$' in field:
                # field contains variable or function
                field = parse_data(
                    field, variables_mapping, functions_mapping
                )
            field_value = self._search_jmespath(field)
            extract_mapping[key] = field_value

        logger.info(f"extract mapping: {extract_mapping}")
        return extract_mapping

    def validate(
        self,
        validators: Validators,
        variables_mapping: VariablesMapping = None,
        functions_mapping: FunctionsMapping = None,
    ):
        variables_mapping = variables_mapping or {}
        functions_mapping = functions_mapping or {}

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
                check_item = parse_data(
                    check_item, variables_mapping, functions_mapping
                )
                check_item = parse_string_value(check_item)

            if check_item and isinstance(check_item, Text):
                check_value = self._search_jmespath(check_item)
            else:
                # variable or function evaluation result is "" or not text
                check_value = check_item

            # comparator
            assert_method = u_validator["assert"]
            if assert_method != 'custom':
                assert_func = get_mapping_function(assert_method, functions_mapping)
            else:
                assert_method = u_validator["custom_comparator"]
                assert_func = get_mapping_function(assert_method, functions_mapping)

            # expect item
            expect_item = u_validator["expect"]
            # parse expected value with config/teststep/extracted variables
            expect_value = parse_data(expect_item, variables_mapping, functions_mapping)

            # message
            message = u_validator["message"]
            # parse message with config/teststep/extracted variables
            message = parse_data(message, variables_mapping, functions_mapping)

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
