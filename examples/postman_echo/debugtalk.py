from httprunner import __version__


def get_httprunner_version():
    return __version__


def sum_two(m, n):
    return m + n


def get_testcase_config_variables():
    return {"foo1": "testcase_config_bar1", "foo2": "testcase_config_bar2"}


def get_testsuite_config_variables():
    return {"foo1": "testsuite_config_bar1", "foo2": "testsuite_config_bar2"}


def get_app_version():
    return [3.1, 3.0]


def calculate_two_nums(a, b=1):
    return [a + b, b - a]


def get_session_fixture(session_fixture):
    return session_fixture[0]["name"]
