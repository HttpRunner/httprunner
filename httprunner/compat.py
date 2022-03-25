"""
This module handles compatibility issues between testcase format v2 and v3.
"""
import os
import sys
from typing import List, Dict, Text, Union, Any

from loguru import logger

from httprunner import exceptions
from httprunner.loader import load_project_meta, convert_relative_project_root_dir
from httprunner.parser import parse_data
from httprunner.utils import sort_dict_by_custom_order


def convert_variables(
    raw_variables: Union[Dict, List, Text], test_path: Text
) -> Dict[Text, Any]:

    if isinstance(raw_variables, Dict):
        return raw_variables

    if isinstance(raw_variables, List):
        # [{"var1": 1}, {"var2": 2}]
        variables: Dict[Text, Any] = {}
        for var_item in raw_variables:
            if not isinstance(var_item, Dict) or len(var_item) != 1:
                raise exceptions.TestCaseFormatError(
                    f"Invalid variables format: {raw_variables}"
                )

            variables.update(var_item)

        return variables

    elif isinstance(raw_variables, Text):
        # get variables by function, e.g. ${get_variables()}
        project_meta = load_project_meta(test_path)
        variables = parse_data(raw_variables, {}, project_meta.functions)

        return variables

    else:
        raise exceptions.TestCaseFormatError(
            f"Invalid variables format: {raw_variables}"
        )


def _convert_jmespath(raw: Text) -> Text:
    if not isinstance(raw, Text):
        raise exceptions.TestCaseFormatError(f"Invalid jmespath extractor: {raw}")

    # content.xx/json.xx => body.xx
    if raw.startswith("content"):
        raw = f"body{raw[len('content'):]}"
    elif raw.startswith("json"):
        raw = f"body{raw[len('json'):]}"

    raw_list = []
    for item in raw.split("."):
        if "-" in item and "[-" not in item:
            # add quotes for field with separator
            # e.g. headers.Content-Type => headers."Content-Type"
            # also need to avoid replacing negative index in jmespath
            # e.g. body.users[-1] => body.users[-1], keep unchanged
            item = item.strip('"')
            raw_list.append(f'"{item}"')
        elif item.isdigit():
            if len(raw_list) == 0:
                logger.error(f"Invalid jmespath: {raw}")
                sys.exit(1)
            last_item = raw_list.pop()
            item = f"{last_item}[{item}]"
            raw_list.append(item)
        else:
            raw_list.append(item)

    return ".".join(raw_list)


def _convert_extractors(extractors: Union[List, Dict]) -> Dict:
    """ convert extract list(v2) to dict(v3)

    Args:
        extractors: [{"varA": "content.varA"}, {"varB": "json.varB"}]

    Returns:
        {"varA": "body.varA", "varB": "body.varB"}

    """
    v3_extractors: Dict = {}

    if isinstance(extractors, List):
        # [{"varA": "content.varA"}, {"varB": "json.varB"}]
        for extractor in extractors:
            if not isinstance(extractor, Dict):
                logger.error(f"Invalid extractor: {extractors}")
                sys.exit(1)
            for k, v in extractor.items():
                v3_extractors[k] = v
    elif isinstance(extractors, Dict):
        # {"varA": "body.varA", "varB": "body.varB"}
        v3_extractors = extractors
    else:
        logger.error(f"Invalid extractor: {extractors}")
        sys.exit(1)

    for k, v in v3_extractors.items():
        v3_extractors[k] = _convert_jmespath(v)

    return v3_extractors


def _convert_validators(validators: List) -> List:
    for v in validators:
        if "check" in v and "expect" in v:
            # format1: {"check": "content.abc", "assert": "eq", "expect": 201}
            v["check"] = _convert_jmespath(v["check"])

        elif len(v) == 1:
            # format2: {'eq': ['status_code', 201]}
            comparator = list(v.keys())[0]
            v[comparator][0] = _convert_jmespath(v[comparator][0])

    return validators


def _sort_request_by_custom_order(request: Dict) -> Dict:
    custom_order = [
        "method",
        "url",
        "params",
        "headers",
        "cookies",
        "data",
        "json",
        "files",
        "timeout",
        "allow_redirects",
        "proxies",
        "verify",
        "stream",
        "auth",
        "cert",
    ]
    return sort_dict_by_custom_order(request, custom_order)


def _sort_step_by_custom_order(step: Dict) -> Dict:
    custom_order = [
        "name",
        "variables",
        "request",
        "testcase",
        "setup_hooks",
        "teardown_hooks",
        "extract",
        "validate",
        "validate_script",
    ]
    return sort_dict_by_custom_order(step, custom_order)


def _ensure_step_attachment(step: Dict) -> Dict:
    test_dict = {
        "name": step["name"],
    }

    if "variables" in step:
        test_dict["variables"] = step["variables"]

    if "setup_hooks" in step:
        test_dict["setup_hooks"] = step["setup_hooks"]

    if "teardown_hooks" in step:
        test_dict["teardown_hooks"] = step["teardown_hooks"]

    if "extract" in step:
        test_dict["extract"] = _convert_extractors(step["extract"])

    if "export" in step:
        test_dict["export"] = step["export"]

    if "validate" in step:
        if not isinstance(step["validate"], List):
            raise exceptions.TestCaseFormatError(
                f'Invalid teststep validate: {step["validate"]}'
            )
        test_dict["validate"] = _convert_validators(step["validate"])

    if "validate_script" in step:
        test_dict["validate_script"] = step["validate_script"]

    return test_dict


def ensure_testcase_v3_api(api_content: Dict) -> Dict:
    logger.info("convert api in v2 to testcase format v3")

    teststep = {
        "request": _sort_request_by_custom_order(api_content["request"]),
    }
    teststep.update(_ensure_step_attachment(api_content))

    teststep = _sort_step_by_custom_order(teststep)

    config = {"name": api_content["name"]}
    extract_variable_names: List = list(teststep.get("extract", {}).keys())
    if extract_variable_names:
        config["export"] = extract_variable_names

    return {
        "config": config,
        "teststeps": [teststep],
    }


def ensure_testcase_v3(test_content: Dict) -> Dict:
    logger.info("ensure compatibility with testcase format v2")

    v3_content = {"config": test_content["config"], "teststeps": []}

    if "teststeps" not in test_content:
        logger.error(f"Miss teststeps: {test_content}")
        sys.exit(1)

    if not isinstance(test_content["teststeps"], list):
        logger.error(
            f'teststeps should be list type, got {type(test_content["teststeps"])}: {test_content["teststeps"]}'
        )
        sys.exit(1)

    for step in test_content["teststeps"]:
        teststep = {}

        if "request" in step:
            teststep["request"] = _sort_request_by_custom_order(step.pop("request"))
        elif "api" in step:
            teststep["testcase"] = step.pop("api")
        elif "testcase" in step:
            teststep["testcase"] = step.pop("testcase")
        else:
            raise exceptions.TestCaseFormatError(f"Invalid teststep: {step}")

        teststep.update(_ensure_step_attachment(step))

        teststep = _sort_step_by_custom_order(teststep)
        v3_content["teststeps"].append(teststep)

    return v3_content


def ensure_cli_args(args: List) -> List:
    """ ensure compatibility with deprecated cli args in v2
    """
    # remove deprecated --failfast
    if "--failfast" in args:
        logger.warning(f"remove deprecated argument: --failfast")
        args.pop(args.index("--failfast"))

    # convert --report-file to --html
    if "--report-file" in args:
        logger.warning(f"replace deprecated argument --report-file with --html")
        index = args.index("--report-file")
        args[index] = "--html"
        args.append("--self-contained-html")

    # keep compatibility with --save-tests in v2
    if "--save-tests" in args:
        logger.warning(
            f"generate conftest.py keep compatibility with --save-tests in v2"
        )
        args.pop(args.index("--save-tests"))
        _generate_conftest_for_summary(args)

    return args


def _generate_conftest_for_summary(args: List):

    for arg in args:
        if os.path.exists(arg):
            test_path = arg
            # FIXME: several test paths maybe specified
            break
    else:
        logger.error(f"No valid test path specified! \nargs: {args}")
        sys.exit(1)

    conftest_content = '''# NOTICE: Generated By HttpRunner.
import json
import os
import time

import pytest
from loguru import logger

from httprunner.utils import get_platform, ExtendJSONEncoder


@pytest.fixture(scope="session", autouse=True)
def session_fixture(request):
    """setup and teardown each task"""
    logger.info(f"start running testcases ...")

    start_at = time.time()

    yield

    logger.info(f"task finished, generate task summary for --save-tests")

    summary = {
        "success": True,
        "stat": {
            "testcases": {"total": 0, "success": 0, "fail": 0},
            "teststeps": {"total": 0, "failures": 0, "successes": 0},
        },
        "time": {"start_at": start_at, "duration": time.time() - start_at},
        "platform": get_platform(),
        "details": [],
    }

    for item in request.node.items:
        testcase_summary = item.instance.get_summary()
        summary["success"] &= testcase_summary.success

        summary["stat"]["testcases"]["total"] += 1
        summary["stat"]["teststeps"]["total"] += len(testcase_summary.step_datas)
        if testcase_summary.success:
            summary["stat"]["testcases"]["success"] += 1
            summary["stat"]["teststeps"]["successes"] += len(
                testcase_summary.step_datas
            )
        else:
            summary["stat"]["testcases"]["fail"] += 1
            summary["stat"]["teststeps"]["successes"] += (
                len(testcase_summary.step_datas) - 1
            )
            summary["stat"]["teststeps"]["failures"] += 1

        testcase_summary_json = testcase_summary.dict()
        testcase_summary_json["records"] = testcase_summary_json.pop("step_datas")
        summary["details"].append(testcase_summary_json)

    summary_path = r"{{SUMMARY_PATH_PLACEHOLDER}}"
    summary_dir = os.path.dirname(summary_path)
    os.makedirs(summary_dir, exist_ok=True)

    with open(summary_path, "w", encoding="utf-8") as f:
        json.dump(summary, f, indent=4, ensure_ascii=False, cls=ExtendJSONEncoder)

    logger.info(f"generated task summary: {summary_path}")

'''

    project_meta = load_project_meta(test_path)
    project_root_dir = project_meta.RootDir
    conftest_path = os.path.join(project_root_dir, "conftest.py")

    test_path = os.path.abspath(test_path)
    logs_dir_path = os.path.join(project_root_dir, "logs")
    test_path_relative_path = convert_relative_project_root_dir(test_path)

    if os.path.isdir(test_path):
        file_foder_path = os.path.join(logs_dir_path, test_path_relative_path)
        dump_file_name = "all.summary.json"
    else:
        file_relative_folder_path, test_file = os.path.split(test_path_relative_path)
        file_foder_path = os.path.join(logs_dir_path, file_relative_folder_path)
        test_file_name, _ = os.path.splitext(test_file)
        dump_file_name = f"{test_file_name}.summary.json"

    summary_path = os.path.join(file_foder_path, dump_file_name)
    conftest_content = conftest_content.replace(
        "{{SUMMARY_PATH_PLACEHOLDER}}", summary_path
    )

    dir_path = os.path.dirname(conftest_path)
    if not os.path.exists(dir_path):
        os.makedirs(dir_path)

    with open(conftest_path, "w", encoding="utf-8") as f:
        f.write(conftest_content)

    logger.info("generated conftest.py to generate summary.json")


def ensure_path_sep(path: Text) -> Text:
    """ ensure compatibility with different path separators of Linux and Windows
    """
    if "/" in path:
        path = os.sep.join(path.split("/"))

    if "\\" in path:
        path = os.sep.join(path.split("\\"))

    return path
