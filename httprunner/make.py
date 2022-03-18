import os
import string
import subprocess
import sys
from typing import Text, List, Tuple, Dict, Set, NoReturn

import jinja2
from loguru import logger
from sentry_sdk import capture_exception

try:
    import allure
    from allure_commons.types import AttachmentType

    USE_ALLURE = True
except ModuleNotFoundError:
    USE_ALLURE = False

from httprunner.parser import function_regex_compile, parse_function_params
from httprunner import exceptions, __version__, Parameters
from httprunner.compat import (
    ensure_testcase_v3_api,
    ensure_testcase_v3,
    convert_variables,
    ensure_path_sep,
)
from httprunner.loader import (
    load_folder_files,
    load_test_file,
    load_testcase,
    load_testsuite,
    load_project_meta,
    convert_relative_project_root_dir,
)
from httprunner.response import uniform_validator
from httprunner.utils import merge_variables, is_support_multiprocessing

""" cache converted pytest files, avoid duplicate making
"""
pytest_files_made_cache_mapping: Dict[Text, Text] = {}

""" save generated pytest files to run, except referenced testcase
"""
pytest_files_run_set: Set = set()

__TEMPLATE__ = jinja2.Template(
    """# NOTE: Generated By HttpRunner v{{ version }}
# FROM: {{ testcase_path }}

{% if imports_list and diff_levels > 0 %}
import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__){% for _ in range(diff_levels) %}.parent{% endfor %}))
{% endif %}

{% if pytest_marks %}
import debugtalk
import pytest
{% endif %}

{% if use_allure %}
import allure
{% endif %}

from httprunner import HttpRunner, Config, Step, RunRequest, RunTestCase
{% for import_str in imports_list %}
{{ import_str }}
{% endfor %}

class {{ class_name }}(HttpRunner):

{{ pytest_marks }}

    config = {{ config_chain_style }}

    teststeps = [
        {% for step_chain_style in teststeps_chain_style %}
            {{ step_chain_style }},
        {% endfor %}
    ]

if __name__ == "__main__":
    {{ class_name }}().test_start()

"""
)


def __ensure_absolute(path: Text) -> Text:
    if path.startswith("./"):
        # Linux/Darwin, hrun ./test.yml
        path = path[len("./"):]
    elif path.startswith(".\\"):
        # Windows, hrun .\\test.yml
        path = path[len(".\\"):]

    path = ensure_path_sep(path)
    project_meta = load_project_meta(path)

    if os.path.isabs(path):
        absolute_path = path
    else:
        absolute_path = os.path.join(project_meta.RootDir, path)

    if not os.path.isfile(absolute_path):
        logger.error(f"Invalid testcase file path: {absolute_path}")
        sys.exit(1)

    return absolute_path


def ensure_file_abs_path_valid(file_abs_path: Text) -> Text:
    """ ensure file path valid for pytest, handle cases when directory name includes dot/hyphen/space

    Args:
        file_abs_path: absolute file path

    Returns:
        ensured valid absolute file path

    """
    project_meta = load_project_meta(file_abs_path)
    raw_abs_file_name, file_suffix = os.path.splitext(file_abs_path)
    file_suffix = file_suffix.lower()

    raw_file_relative_name = convert_relative_project_root_dir(raw_abs_file_name)
    if raw_file_relative_name == "":
        return file_abs_path

    path_names = []
    for name in raw_file_relative_name.rstrip(os.sep).split(os.sep):

        if name[0] in string.digits:
            # ensure file name not startswith digit
            # 19 => T19, 2C => T2C
            name = f"T{name}"

        if name.startswith("."):
            # avoid ".csv" been converted to "_csv"
            pass
        else:
            # handle cases when directory name includes dot/hyphen/space
            name = name.replace(" ", "_").replace(".", "_").replace("-", "_")

        path_names.append(name)

    new_file_path = os.path.join(
        project_meta.RootDir, f"{os.sep.join(path_names)}{file_suffix}"
    )
    return new_file_path


def __ensure_testcase_module(path: Text) -> NoReturn:
    """ ensure pytest files are in python module, generate __init__.py on demand
    """
    init_file = os.path.join(os.path.dirname(path), "__init__.py")
    if os.path.isfile(init_file):
        return

    with open(init_file, "w", encoding="utf-8") as f:
        f.write("# NOTICE: Generated By HttpRunner. DO NOT EDIT!\n")


def convert_testcase_path(testcase_abs_path: Text) -> Tuple[Text, Text]:
    """convert single YAML/JSON testcase path to python file"""
    testcase_new_path = ensure_file_abs_path_valid(testcase_abs_path)

    dir_path = os.path.dirname(testcase_new_path)
    file_name, _ = os.path.splitext(os.path.basename(testcase_new_path))
    testcase_python_abs_path = os.path.join(dir_path, f"{file_name}_test.py")

    # convert title case, e.g. request_with_variables => RequestWithVariables
    name_in_title_case = file_name.title().replace("_", "")

    return testcase_python_abs_path, name_in_title_case


def format_pytest_with_black(*python_paths: Text) -> NoReturn:
    logger.info("format pytest cases with black ...")
    try:
        if is_support_multiprocessing() or len(python_paths) <= 1:
            subprocess.run(["black", *python_paths])
        else:
            logger.warning(
                "this system does not support multiprocessing well, format files one by one ..."
            )
            [subprocess.run(["black", path]) for path in python_paths]
    except subprocess.CalledProcessError as ex:
        capture_exception(ex)
        logger.error(ex)
        sys.exit(1)
    except FileNotFoundError:
        err_msg = """
missing dependency tool: black
install black manually and try again:
$ pip install black
"""
        logger.error(err_msg)
        sys.exit(1)


def make_config_chain_style(config: Dict) -> Text:
    config_chain_style = f'Config("{config["name"]}")'

    if config["variables"]:
        variables = config["variables"]
        config_chain_style += f".variables(**{variables})"

    if "base_url" in config:
        config_chain_style += f'.base_url("{config["base_url"]}")'

    if "verify" in config:
        config_chain_style += f'.verify({config["verify"]})'

    if "export" in config:
        config_chain_style += f'.export(*{config["export"]})'

    if "weight" in config:
        config_chain_style += f'.locust_weight({config["weight"]})'

    return config_chain_style


def make_request_chain_style(request: Dict) -> Text:
    method = request["method"].lower()
    url = request["url"]
    request_chain_style = f'.{method}("{url}")'

    if "params" in request:
        params = request["params"]
        request_chain_style += f".with_params(**{params})"

    if "headers" in request:
        headers = request["headers"]
        request_chain_style += f".with_headers(**{headers})"

    if "cookies" in request:
        cookies = request["cookies"]
        request_chain_style += f".with_cookies(**{cookies})"

    if "data" in request:
        data = request["data"]
        if isinstance(data, Text):
            data = f'"{data}"'
        request_chain_style += f".with_data({data})"

    if "json" in request:
        req_json = request["json"]
        if isinstance(req_json, Text):
            req_json = f'"{req_json}"'
        request_chain_style += f".with_json({req_json})"

    if "timeout" in request:
        timeout = request["timeout"]
        request_chain_style += f".set_timeout({timeout})"

    if "verify" in request:
        verify = request["verify"]
        request_chain_style += f".set_verify({verify})"

    if "allow_redirects" in request:
        allow_redirects = request["allow_redirects"]
        request_chain_style += f".set_allow_redirects({allow_redirects})"

    if "upload" in request:
        upload = request["upload"]
        request_chain_style += f".upload(**{upload})"

    return request_chain_style


def make_teststep_chain_style(teststep: Dict) -> Text:
    if teststep.get("request"):
        step_info = f'RunRequest("{teststep["name"]}")'
    elif teststep.get("testcase"):
        step_info = f'RunTestCase("{teststep["name"]}")'
    else:
        raise exceptions.TestCaseFormatError(f"Invalid teststep: {teststep}")

    if "variables" in teststep:
        variables = teststep["variables"]
        step_info += f".with_variables(**{variables})"

    if "retry" in teststep:
        tries = teststep["retry"]["tries"]
        delay = teststep["retry"]["delay"]
        step_info += f".with_retry(tries={tries}, delay={delay})"

    if "setup_hooks" in teststep:
        setup_hooks = teststep["setup_hooks"]
        for hook in setup_hooks:
            if isinstance(hook, Text):
                step_info += f'.setup_hook("{hook}")'
            elif isinstance(hook, Dict) and len(hook) == 1:
                assign_var_name, hook_content = list(hook.items())[0]
                step_info += f'.setup_hook("{hook_content}", "{assign_var_name}")'
            else:
                raise exceptions.TestCaseFormatError(f"Invalid setup hook: {hook}")

    if teststep.get("request"):
        step_info += make_request_chain_style(teststep["request"])
    elif teststep.get("testcase"):
        testcase = teststep["testcase"]
        call_ref_testcase = f".call({testcase})"
        step_info += call_ref_testcase

    if "teardown_hooks" in teststep:
        teardown_hooks = teststep["teardown_hooks"]
        for hook in teardown_hooks:
            if isinstance(hook, Text):
                step_info += f'.teardown_hook("{hook}")'
            elif isinstance(hook, Dict) and len(hook) == 1:
                assign_var_name, hook_content = list(hook.items())[0]
                step_info += f'.teardown_hook("{hook_content}", "{assign_var_name}")'
            else:
                raise exceptions.TestCaseFormatError(f"Invalid teardown hook: {hook}")

    if "extract" in teststep:
        # request step
        step_info += ".extract()"
        for extract_name, extract_path in teststep["extract"].items():
            step_info += f""".with_jmespath('{extract_path}', '{extract_name}')"""

    if "export" in teststep:
        # reference testcase step
        export: List[Text] = teststep["export"]
        step_info += f".export(*{export})"

    if "validate" in teststep:
        step_info += ".validate()"

        for v in teststep["validate"]:
            validator = uniform_validator(v)
            assert_method = validator["assert"]
            check = validator["check"]
            if '"' in check:
                # e.g. body."user-agent" => 'body."user-agent"'
                check = f"'{check}'"
            else:
                check = f'"{check}"'
            expect = validator["expect"]
            if isinstance(expect, Text):
                expect = f'"{expect}"'

            message = validator["message"]
            if message:
                step_info += f".assert_{assert_method}({check}, {expect}, '{message}')"
            else:
                step_info += f".assert_{assert_method}({check}, {expect})"

    return f"Step({step_info})"


def make_testcase(testcase: Dict, dir_path: Text = None) -> Text:
    """convert valid testcase dict to pytest file path"""
    # ensure compatibility with testcase format v2
    testcase = ensure_testcase_v3(testcase)

    # validate testcase format
    load_testcase(testcase)

    testcase_abs_path = __ensure_absolute(testcase["config"]["path"])
    logger.info(f"start to make testcase: {testcase_abs_path}")

    testcase_python_abs_path, testcase_cls_name = convert_testcase_path(
        testcase_abs_path
    )
    if dir_path:
        testcase_python_abs_path = os.path.join(
            dir_path, os.path.basename(testcase_python_abs_path)
        )

    global pytest_files_made_cache_mapping
    if testcase_python_abs_path in pytest_files_made_cache_mapping:
        return testcase_python_abs_path

    config = testcase["config"]
    config["path"] = convert_relative_project_root_dir(testcase_python_abs_path)
    config["variables"] = convert_variables(
        config.get("variables", {}), testcase_abs_path
    )

    # prepare reference testcase
    imports_list = []
    teststeps = testcase["teststeps"]
    for teststep in teststeps:
        if not teststep.get("testcase"):
            continue

        # make ref testcase pytest file
        ref_testcase_path = __ensure_absolute(teststep["testcase"])
        test_content = load_test_file(ref_testcase_path)

        if not isinstance(test_content, Dict):
            raise exceptions.TestCaseFormatError(f"Invalid teststep: {teststep}")

        # api in v2 format, convert to v3 testcase
        if "request" in test_content and "name" in test_content:
            test_content = ensure_testcase_v3_api(test_content)

        test_content.setdefault("config", {})["path"] = ref_testcase_path
        ref_testcase_python_abs_path = make_testcase(test_content)

        # override testcase export
        ref_testcase_export: List = test_content["config"].get("export", [])
        if ref_testcase_export:
            step_export: List = teststep.setdefault("export", [])
            step_export.extend(ref_testcase_export)
            teststep["export"] = list(set(step_export))

        # prepare ref testcase class name
        ref_testcase_cls_name = pytest_files_made_cache_mapping[
            ref_testcase_python_abs_path
        ]
        teststep["testcase"] = ref_testcase_cls_name

        # prepare import ref testcase
        ref_testcase_python_relative_path = convert_relative_project_root_dir(
            ref_testcase_python_abs_path
        )
        ref_module_name, _ = os.path.splitext(ref_testcase_python_relative_path)
        ref_module_name = ref_module_name.replace(os.sep, ".")
        import_expr = f"from {ref_module_name} import TestCase{ref_testcase_cls_name} as {ref_testcase_cls_name}"
        if import_expr not in imports_list:
            imports_list.append(import_expr)

    testcase_path = convert_relative_project_root_dir(testcase_abs_path)
    # current file compared to ProjectRootDir
    diff_levels = len(testcase_path.split(os.sep))

    data = {
        "version": __version__,
        "testcase_path": testcase_path,
        "diff_levels": diff_levels,
        "class_name": f"TestCase{testcase_cls_name}",
        "imports_list": imports_list,
        "config_chain_style": make_config_chain_style(config),
        "pytest_marks": make_pytest_marks(config),
        "use_allure": USE_ALLURE,
        "teststeps_chain_style": [
            make_teststep_chain_style(step) for step in teststeps
        ],
    }
    content = __TEMPLATE__.render(data)

    # ensure new file's directory exists
    dir_path = os.path.dirname(testcase_python_abs_path)
    if not os.path.exists(dir_path):
        os.makedirs(dir_path)

    with open(testcase_python_abs_path, "w", encoding="utf-8") as f:
        f.write(content)

    pytest_files_made_cache_mapping[testcase_python_abs_path] = testcase_cls_name
    __ensure_testcase_module(testcase_python_abs_path)

    logger.info(f"generated testcase: {testcase_python_abs_path}")

    return testcase_python_abs_path


def make_testsuite(testsuite: Dict) -> NoReturn:
    """convert valid testsuite dict to pytest folder with testcases"""
    # validate testsuite format
    load_testsuite(testsuite)

    testsuite_config = testsuite["config"]
    testsuite_path = testsuite_config["path"]
    testsuite_variables = convert_variables(
        testsuite_config.get("variables", {}), testsuite_path
    )

    logger.info(f"start to make testsuite: {testsuite_path}")

    # create directory with testsuite file name, put its testcases under this directory
    testsuite_path = ensure_file_abs_path_valid(testsuite_path)
    testsuite_dir, file_suffix = os.path.splitext(testsuite_path)
    # demo_testsuite.yml => demo_testsuite_yml
    testsuite_dir = f"{testsuite_dir}_{file_suffix.lstrip('.')}"

    for testcase in testsuite["testcases"]:
        # get referenced testcase content
        testcase_file = testcase["testcase"]
        testcase_path = __ensure_absolute(testcase_file)
        testcase_dict = load_test_file(testcase_path)
        testcase_dict.setdefault("config", {})
        testcase_dict["config"]["path"] = testcase_path

        # override testcase name
        testcase_dict["config"]["name"] = testcase["name"]
        # override base_url
        base_url = testsuite_config.get("base_url") or testcase.get("base_url")
        if base_url:
            testcase_dict["config"]["base_url"] = base_url
        # override verify
        if "verify" in testsuite_config:
            testcase_dict["config"]["verify"] = testsuite_config["verify"]
        # override variables
        # testsuite testcase variables > testsuite config variables
        testcase_variables = convert_variables(
            testcase.get("variables", {}), testcase_path
        )
        testcase_variables = merge_variables(testcase_variables, testsuite_variables)
        # testsuite testcase variables > testcase config variables
        testcase_dict["config"]["variables"] = convert_variables(
            testcase_dict["config"].get("variables", {}), testcase_path
        )
        testcase_dict["config"]["variables"].update(testcase_variables)

        # override weight
        if "weight" in testcase:
            testcase_dict["config"]["weight"] = testcase["weight"]

        # make testcase
        testcase_pytest_path = make_testcase(testcase_dict, testsuite_dir)
        pytest_files_run_set.add(testcase_pytest_path)


def make_pytest_marks(config: Dict) -> (Text, Text):
    project_meta = load_project_meta(config["path"])

    # pytest marks, refer:
    # https://docs.pytest.org/en/6.2.x/mark.html
    # https://docs.pytest.org/en/6.2.x/reference.html
    result = ""
    default_test_start = (
        "    def test_start(self):    \n"
        "        super().test_start() \n"
    )
    fixtures_test_start = (
        "    def test_start(self, {fixture_str}):                         \n"
        "        scope, fixtures = locals(), {fixture_list}               \n"
        "        values = map(lambda x: scope.get(x), fixtures)           \n"
        "        super().test_start(fixtures=dict(zip(fixtures, values))) \n"
    )
    parameters_test_start = (
        "    def test_start(self, param):        \n"
        "        super().test_start(param=param) \n"
    )
    both_test_start = (
        "    def test_start(self, {fixture_str}, param):                                 \n"
        "        scope, fixtures = locals(), {fixture_list}                              \n"
        "        values = map(lambda x: scope.get(x), fixtures)                          \n"
        "        super().test_start(fixtures=dict(zip({fixtures}, values)), param=param) \n"
    )

    # marks
    # keep yaml load order, refer:
    # https://stackoverflow.com/a/47881325
    # https://mail.python.org/pipermail/python-dev/2016-September/146327.html
    fixture_list = []
    fixtures_exist = False
    parameters_exist = False
    for key, param in config.items():

        if key == "filterwarnings":
            result += f"    @pytest.mark.filterwarnings({param})\n"

        elif key == "parameters":
            parameters_exist = True
            param = Parameters(param)
            result += f"    @pytest.mark.parametrize(\"param\", {param})\n"

        elif key == "skip":
            result += f"    @pytest.mark.skip(reason=\"{param}\")\n"

        elif key == "usefixtures":
            errmsg = f"usefixtures type should be List[str], got ({type(param)}, {param})"
            assert isinstance(param, list), errmsg
            fixture_list.extend(param)
            fixtures_exist = True

        elif key == "custom_marks":
            errmsg = f"custom_marks type should be List[str], got ({type(param)}, {param})"
            assert isinstance(param, list), errmsg
            for mark in param:
                result += f"    @pytest.mark.{mark}\n"

        elif USE_ALLURE and key == "links":
            errmsg = f"links type should be List[dict], got ({type(param)}, {param})"
            assert isinstance(param, list), errmsg
            assert param and isinstance(param[0], dict), errmsg
            for link in reversed(param):
                if link.get("name"):
                    result += f"    @allure.link(\"{link['url']}\", name=\"{link['name']}\")\n"
                else:
                    result += f"    @allure.link(\"{link['url']}\")\n"

        elif USE_ALLURE and key == "description":
            result += f"    @allure.description(\"{param}\")\n"

        elif USE_ALLURE and key == "name":
            result += f"    @allure.title(\"{param}\")\n"

        elif key in ("skipif", "xfail"):
            condition = param.get("condition")
            func_match = function_regex_compile.match(condition)
            if func_match:
                func_name = func_match.group(1)
                func_params_str = func_match.group(2)
                func_meta = parse_function_params(func_params_str)
                if project_meta.functions.get(func_name):
                    condition = f"debugtalk.{func_name}(*{func_meta['args']}, **{func_meta['kwargs']})"

            if key == "skipif":
                reason = param.get("reason")
                result += f"    @pytest.mark.skipif({condition}, reason=\"{reason}\")\n"

            if key == "xfail":
                reason = param.get("reason")
                raises = param.get("raises", None)
                run = param.get("run", True)
                strict = param.get("strict", False)
                result += (f"    @pytest.mark.skipif({condition}, reason=\"{reason}\", "
                           f"raises={raises}, run={run}, strict={strict})\n")

    if not result:
        return result

    # test_start
    if parameters_exist and fixtures_exist:
        result += both_test_start.format(
            fixture_str=", ".join(fixture_list),
            fixture_list=fixture_list
        )
    elif fixtures_exist:
        result += fixtures_test_start.format(
            fixture_str=", ".join(fixture_list),
            fixture_list=fixture_list
        )
    elif parameters_exist:
        result += parameters_test_start
    else:
        result += default_test_start

    return result


def __make(tests_path: Text) -> NoReturn:
    """ make testcase(s) with testcase/testsuite/folder absolute path
        generated pytest file path will be cached in pytest_files_made_cache_mapping

    Args:
        tests_path: should be in absolute path

    """
    logger.info(f"make path: {tests_path}")
    test_files = []
    if os.path.isdir(tests_path):
        files_list = load_folder_files(tests_path)
        test_files.extend(files_list)
    elif os.path.isfile(tests_path):
        test_files.append(tests_path)
    else:
        raise exceptions.TestcaseNotFound(f"Invalid tests path: {tests_path}")

    for test_file in test_files:
        if test_file.lower().endswith("_test.py"):
            pytest_files_run_set.add(test_file)
            continue

        try:
            test_content = load_test_file(test_file)
        except (exceptions.FileNotFound, exceptions.FileFormatError) as ex:
            logger.warning(f"Invalid test file: {test_file}\n{type(ex).__name__}: {ex}")
            continue

        if not isinstance(test_content, Dict):
            logger.warning(
                f"Invalid test file: {test_file}\n"
                f"reason: test content not in dict format."
            )
            continue

        # api in v2 format, convert to v3 testcase
        if "request" in test_content and "name" in test_content:
            test_content = ensure_testcase_v3_api(test_content)

        if "config" not in test_content:
            logger.warning(
                f"Invalid testcase/testsuite file: {test_file}\n"
                f"reason: missing config part."
            )
            continue
        elif not isinstance(test_content["config"], Dict):
            logger.warning(
                f"Invalid testcase/testsuite file: {test_file}\n"
                f"reason: config should be dict type, got {test_content['config']}"
            )
            continue

        # ensure path absolute
        test_content.setdefault("config", {})["path"] = test_file

        # testcase
        if "teststeps" in test_content:
            try:
                testcase_pytest_path = make_testcase(test_content)
                pytest_files_run_set.add(testcase_pytest_path)
            except exceptions.TestCaseFormatError as ex:
                logger.warning(
                    f"Invalid testcase file: {test_file}\n{type(ex).__name__}: {ex}"
                )
                continue

        # testsuite
        elif "testcases" in test_content:
            try:
                make_testsuite(test_content)
            except exceptions.TestSuiteFormatError as ex:
                logger.warning(
                    f"Invalid testsuite file: {test_file}\n{type(ex).__name__}: {ex}"
                )
                continue

        # invalid format
        else:
            logger.warning(
                f"Invalid test file: {test_file}\n"
                f"reason: file content is neither testcase nor testsuite"
            )


def main_make(tests_paths: List[Text]) -> List[Text]:
    if not tests_paths:
        return []

    for tests_path in tests_paths:
        tests_path = ensure_path_sep(tests_path)
        if not os.path.isabs(tests_path):
            tests_path = os.path.join(os.getcwd(), tests_path)

        try:
            __make(tests_path)
        except exceptions.MyBaseError as ex:
            logger.error(ex)
            sys.exit(1)

    # format pytest files
    pytest_files_format_list = pytest_files_made_cache_mapping.keys()
    format_pytest_with_black(*pytest_files_format_list)

    return list(pytest_files_run_set)


def init_make_parser(subparsers):
    """ make testcases: parse command line options and run commands.
    """
    parser = subparsers.add_parser(
        "make", help="Convert YAML/JSON testcases to pytest cases.",
    )
    parser.add_argument(
        "testcase_path", nargs="*", help="Specify YAML/JSON testcase file/folder path"
    )

    return parser
