import os
import unittest

from httprunner import loader
from httprunner.cli import main_run
from httprunner.runner import HttpRunner


class TestHttpRunner(unittest.TestCase):
    def setUp(self):
        loader.project_meta = None
        self.runner = HttpRunner()

    def test_run_testcase_by_path_request_only(self):
        self.runner.run_path(
            "examples/postman_echo/request_methods/request_with_functions.yml"
        )
        result = self.runner.get_summary()
        self.assertTrue(result.success)
        self.assertEqual(result.name, "request methods testcase with functions")
        self.assertEqual(result.step_datas[0].name, "get with params")
        self.assertEqual(len(result.step_datas), 3)

    def test_run_testcase_by_path_ref_testcase(self):
        self.runner.run_path(
            "examples/postman_echo/request_methods/request_with_testcase_reference.yml"
        )
        result = self.runner.get_summary()
        self.assertTrue(result.success)
        self.assertEqual(result.name, "request methods testcase: reference testcase")
        self.assertEqual(result.step_datas[0].name, "request with functions")
        self.assertEqual(len(result.step_datas), 2)

    def test_run_testcase_with_abnormal_path(self):
        self.runner.run_path("tests/data/a-b.c/2 3.yml")
        result = self.runner.get_summary()
        self.assertTrue(result.success)
        self.assertTrue(os.path.exists("tests/data/a_b_c/__init__.py"))
        self.assertTrue(os.path.exists("tests/data/debugtalk.py"))
        self.assertTrue(os.path.exists("tests/data/a_b_c/T1_test.py"))
        self.assertTrue(os.path.exists("tests/data/a_b_c/T2_3_test.py"))
