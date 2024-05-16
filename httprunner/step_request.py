import json
import time
from typing import Any, Dict, List, Text, Union

import requests
from loguru import logger

from httprunner import utils
from httprunner.exceptions import ValidationFailure
from httprunner.ext.uploader import prepare_upload_step
from httprunner.models import (
    Hooks,
    IStep,
    MethodEnum,
    StepResult,
    TRequest,
    TStep,
    VariablesMapping,
)
from httprunner.parser import build_url, parse_variables_mapping
from httprunner.response import ResponseObject
from httprunner.runner import ALLURE, HttpRunner


def call_hooks(
    runner: HttpRunner, hooks: Hooks, step_variables: VariablesMapping, hook_msg: Text
):
    """call hook actions.

    Args:
        hooks (list): each hook in hooks list maybe in two format.

            format1 (str): only call hook functions.
                ${func()}
            format2 (dict): assignment, the value returned by hook function will be assigned to variable.
                {"var": "${func()}"}

        step_variables: current step variables to call hook, include two special variables

            request: parsed request dict
            response: ResponseObject for current response

        hook_msg: setup/teardown request/testcase

    """
    logger.info(f"call hook actions: {hook_msg}")

    if not isinstance(hooks, List):
        logger.error(f"Invalid hooks format: {hooks}")
        return

    for hook in hooks:
        if isinstance(hook, Text):
            # format 1: ["${func()}"]
            logger.debug(f"call hook function: {hook}")
            runner.parser.parse_data(hook, step_variables)
        elif isinstance(hook, Dict) and len(hook) == 1:
            # format 2: {"var": "${func()}"}
            var_name, hook_content = list(hook.items())[0]
            hook_content_eval = runner.parser.parse_data(hook_content, step_variables)
            logger.debug(
                f"call hook function: {hook_content}, got value: {hook_content_eval}"
            )
            logger.debug(f"assign variable: {var_name} = {hook_content_eval}")
            step_variables[var_name] = hook_content_eval
        else:
            logger.error(f"Invalid hook format: {hook}")


def pretty_format(v) -> str:
    if isinstance(v, dict):
        return json.dumps(v, indent=4, ensure_ascii=False)

    if isinstance(v, requests.structures.CaseInsensitiveDict):
        return json.dumps(dict(v.items()), indent=4, ensure_ascii=False)

    return repr(utils.omit_long_data(v))


def run_step_request(runner: HttpRunner, step: TStep) -> StepResult:
    """run teststep: request"""
    step_result = StepResult(
        name=step.name,
        step_type="request",
        success=False,
    )
    start_time = time.time()

    # parse
    functions = runner.parser.functions_mapping
    step_variables = runner.merge_step_variables(step.variables)
    prepare_upload_step(step, step_variables, functions)
    # parse variables
    step_variables = parse_variables_mapping(step_variables, functions)

    request_dict = step.request.dict()
    request_dict.pop("upload", None)
    parsed_request_dict = runner.parser.parse_data(request_dict, step_variables)

    request_headers = parsed_request_dict.pop("headers", {})
    # omit pseudo header names for HTTP/1, e.g. :authority, :method, :path, :scheme
    request_headers = {
        key: request_headers[key] for key in request_headers if not key.startswith(":")
    }
    request_headers[
        "HRUN-Request-ID"
    ] = f"HRUN-{runner.case_id}-{str(int(time.time() * 1000))[-6:]}"
    parsed_request_dict["headers"] = request_headers

    step_variables["request"] = parsed_request_dict

    # setup hooks
    if step.setup_hooks:
        call_hooks(runner, step.setup_hooks, step_variables, "setup request")

    # prepare arguments
    config = runner.get_config()
    method = parsed_request_dict.pop("method")
    url_path = parsed_request_dict.pop("url")
    url = build_url(config.base_url, url_path)
    parsed_request_dict["verify"] = config.verify
    json_data = parsed_request_dict.pop("req_json", {})
    if json_data and not (isinstance(json_data, dict) or isinstance(json_data, list)):
        json_data = json.loads(json_data)
    parsed_request_dict["json"] = json_data

    # log request
    request_print = "====== request details ======\n"
    request_print += f"url: {url}\n"
    request_print += f"method: {method}\n"
    for k, v in parsed_request_dict.items():
        request_print += f"{k}: {pretty_format(v)}\n"

    logger.debug(request_print)
    if ALLURE is not None:
        ALLURE.attach(
            request_print,
            name="request details",
            attachment_type=ALLURE.attachment_type.TEXT,
        )
    resp = runner.session.request(method, url, **parsed_request_dict)

    # log response
    response_print = "====== response details ======\n"
    response_print += f"status_code: {resp.status_code}\n"
    response_print += f"headers: {pretty_format(resp.headers)}\n"

    try:
        resp_body = resp.json()
    except (requests.exceptions.JSONDecodeError, json.decoder.JSONDecodeError):
        resp_body = resp.content

    response_print += f"body: {pretty_format(resp_body)}\n"
    logger.debug(response_print)
    if ALLURE is not None:
        ALLURE.attach(
            response_print,
            name="response details",
            attachment_type=ALLURE.attachment_type.TEXT,
        )
    resp_obj = ResponseObject(resp, runner.parser)
    step_variables["response"] = resp_obj

    # teardown hooks
    if step.teardown_hooks:
        call_hooks(runner, step.teardown_hooks, step_variables, "teardown request")

    # extract
    extractors = step.extract
    extract_mapping = resp_obj.extract(extractors, step_variables)
    step_result.export_vars = extract_mapping

    variables_mapping = step_variables
    variables_mapping.update(extract_mapping)

    # validate
    validators = step.validators
    try:
        resp_obj.validate(validators, variables_mapping)
        step_result.success = True
    except ValidationFailure:
        raise
    finally:
        session_data = runner.session.data
        session_data.success = step_result.success
        session_data.validators = resp_obj.validation_results

        # save step data
        step_result.data = session_data
        step_result.elapsed = time.time() - start_time

    return step_result


class StepRequestValidation(IStep):
    def __init__(self, step: TStep):
        self.__step = step

    def assert_equal(
        self, jmes_path: Text, expected_value: Any, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append({"equal": [jmes_path, expected_value, message]})
        return self

    def assert_not_equal(
        self, jmes_path: Text, expected_value: Any, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"not_equal": [jmes_path, expected_value, message]}
        )
        return self

    def assert_greater_than(
        self, jmes_path: Text, expected_value: Union[int, float], message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"greater_than": [jmes_path, expected_value, message]}
        )
        return self

    def assert_less_than(
        self, jmes_path: Text, expected_value: Union[int, float], message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"less_than": [jmes_path, expected_value, message]}
        )
        return self

    def assert_greater_or_equals(
        self, jmes_path: Text, expected_value: Union[int, float], message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"greater_or_equals": [jmes_path, expected_value, message]}
        )
        return self

    def assert_less_or_equals(
        self, jmes_path: Text, expected_value: Union[int, float], message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"less_or_equals": [jmes_path, expected_value, message]}
        )
        return self

    def assert_length_equal(
        self, jmes_path: Text, expected_value: int, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"length_equal": [jmes_path, expected_value, message]}
        )
        return self

    def assert_length_greater_than(
        self, jmes_path: Text, expected_value: int, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"length_greater_than": [jmes_path, expected_value, message]}
        )
        return self

    def assert_length_less_than(
        self, jmes_path: Text, expected_value: int, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"length_less_than": [jmes_path, expected_value, message]}
        )
        return self

    def assert_length_greater_or_equals(
        self, jmes_path: Text, expected_value: int, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"length_greater_or_equals": [jmes_path, expected_value, message]}
        )
        return self

    def assert_length_less_or_equals(
        self, jmes_path: Text, expected_value: int, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"length_less_or_equals": [jmes_path, expected_value, message]}
        )
        return self

    def assert_string_equals(
        self, jmes_path: Text, expected_value: Any, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"string_equals": [jmes_path, expected_value, message]}
        )
        return self

    def assert_startswith(
        self, jmes_path: Text, expected_value: Text, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"startswith": [jmes_path, expected_value, message]}
        )
        return self

    def assert_endswith(
        self, jmes_path: Text, expected_value: Text, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"endswith": [jmes_path, expected_value, message]}
        )
        return self

    def assert_regex_match(
        self, jmes_path: Text, expected_value: Text, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"regex_match": [jmes_path, expected_value, message]}
        )
        return self

    def assert_contains(
        self, jmes_path: Text, expected_value: Any, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"contains": [jmes_path, expected_value, message]}
        )
        return self

    def assert_contained_by(
        self, jmes_path: Text, expected_value: Any, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"contained_by": [jmes_path, expected_value, message]}
        )
        return self

    def assert_type_match(
        self, jmes_path: Text, expected_value: Any, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {"type_match": [jmes_path, expected_value, message]}
        )
        return self


    def assert_custom(
        self, custom_function: Text, jmes_path:Text,expected_value: Any, message: Text = ""
    ) -> "StepRequestValidation":
        self.__step.validators.append(
            {custom_function: [jmes_path, expected_value, message]}
        )
        return self

    def struct(self) -> TStep:
        return self.__step

    def name(self) -> Text:
        return self.__step.name

    def type(self) -> Text:
        return f"request-{self.__step.request.method}"

    def run(self, runner: HttpRunner):
        return run_step_request(runner, self.__step)


class StepRequestExtraction(IStep):
    def __init__(self, step: TStep):
        self.__step = step

    def with_jmespath(self, jmes_path: Text, var_name: Text) -> "StepRequestExtraction":
        self.__step.extract[var_name] = jmes_path
        return self

    # def with_regex(self):
    #     # TODO: extract response html with regex
    #     pass
    #
    # def with_jsonpath(self):
    #     # TODO: extract response json with jsonpath
    #     pass

    def validate(self) -> StepRequestValidation:
        return StepRequestValidation(self.__step)

    def struct(self) -> TStep:
        return self.__step

    def name(self) -> Text:
        return self.__step.name

    def type(self) -> Text:
        return f"request-{self.__step.request.method}"

    def run(self, runner: HttpRunner):
        return run_step_request(runner, self.__step)


class RequestWithOptionalArgs(IStep):
    def __init__(self, step: TStep):
        self.__step = step

    def with_params(self, **params) -> "RequestWithOptionalArgs":
        self.__step.request.params.update(params)
        return self

    def with_headers(self, **headers) -> "RequestWithOptionalArgs":
        self.__step.request.headers.update(headers)
        return self

    def with_cookies(self, **cookies) -> "RequestWithOptionalArgs":
        self.__step.request.cookies.update(cookies)
        return self

    def with_data(self, data) -> "RequestWithOptionalArgs":
        self.__step.request.data = data
        return self

    def with_json(self, req_json) -> "RequestWithOptionalArgs":
        self.__step.request.req_json = req_json
        return self

    def set_timeout(self, timeout: float) -> "RequestWithOptionalArgs":
        self.__step.request.timeout = timeout
        return self

    def set_verify(self, verify: bool) -> "RequestWithOptionalArgs":
        self.__step.request.verify = verify
        return self

    def set_allow_redirects(self, allow_redirects: bool) -> "RequestWithOptionalArgs":
        self.__step.request.allow_redirects = allow_redirects
        return self

    def upload(self, **file_info) -> "RequestWithOptionalArgs":
        self.__step.request.upload.update(file_info)
        return self

    def teardown_hook(
        self, hook: Text, assign_var_name: Text = None
    ) -> "RequestWithOptionalArgs":
        if assign_var_name:
            self.__step.teardown_hooks.append({assign_var_name: hook})
        else:
            self.__step.teardown_hooks.append(hook)

        return self

    def extract(self) -> StepRequestExtraction:
        return StepRequestExtraction(self.__step)

    def validate(self) -> StepRequestValidation:
        return StepRequestValidation(self.__step)

    def struct(self) -> TStep:
        return self.__step

    def name(self) -> Text:
        return self.__step.name

    def type(self) -> Text:
        return f"request-{self.__step.request.method}"

    def run(self, runner: HttpRunner):
        return run_step_request(runner, self.__step)


class RunRequest(object):
    def __init__(self, name: Text):
        self.__step = TStep(name=name)

    def with_variables(self, **variables) -> "RunRequest":
        self.__step.variables.update(variables)
        return self

    def with_retry(self, retry_times, retry_interval) -> "RunRequest":
        self.__step.retry_times = retry_times
        self.__step.retry_interval = retry_interval
        return self

    def setup_hook(self, hook: Text, assign_var_name: Text = None) -> "RunRequest":
        if assign_var_name:
            self.__step.setup_hooks.append({assign_var_name: hook})
        else:
            self.__step.setup_hooks.append(hook)

        return self

    def get(self, url: Text) -> RequestWithOptionalArgs:
        self.__step.request = TRequest(method=MethodEnum.GET, url=url)
        return RequestWithOptionalArgs(self.__step)

    def post(self, url: Text) -> RequestWithOptionalArgs:
        self.__step.request = TRequest(method=MethodEnum.POST, url=url)
        return RequestWithOptionalArgs(self.__step)

    def put(self, url: Text) -> RequestWithOptionalArgs:
        self.__step.request = TRequest(method=MethodEnum.PUT, url=url)
        return RequestWithOptionalArgs(self.__step)

    def head(self, url: Text) -> RequestWithOptionalArgs:
        self.__step.request = TRequest(method=MethodEnum.HEAD, url=url)
        return RequestWithOptionalArgs(self.__step)

    def delete(self, url: Text) -> RequestWithOptionalArgs:
        self.__step.request = TRequest(method=MethodEnum.DELETE, url=url)
        return RequestWithOptionalArgs(self.__step)

    def options(self, url: Text) -> RequestWithOptionalArgs:
        self.__step.request = TRequest(method=MethodEnum.OPTIONS, url=url)
        return RequestWithOptionalArgs(self.__step)

    def patch(self, url: Text) -> RequestWithOptionalArgs:
        self.__step.request = TRequest(method=MethodEnum.PATCH, url=url)
        return RequestWithOptionalArgs(self.__step)
