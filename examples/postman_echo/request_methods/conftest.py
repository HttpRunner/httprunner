import uuid
from typing import List

import pytest
from httprunner import Config, Step
from loguru import logger


@pytest.fixture(scope="session", autouse=True)
def session_fixture(request):
    """setup and teardown each task"""
    total_testcases_num = request.node.testscollected
    testcases = []
    for item in request.node.items:
        testcase = {
            "name": item.cls.config.name,
            "path": item.cls.config.path,
            "node_id": item.nodeid,
        }
        testcases.append(testcase)

    logger.debug(f"collected {total_testcases_num} testcases: {testcases}")

    yield testcases

    logger.debug(f"teardown task fixture")

    # teardown task
    # TODO: upload task summary


@pytest.fixture(scope="function", autouse=True)
def testcase_fixture(request):
    """setup and teardown each testcase"""
    config: Config = request.cls.config
    teststeps: List[Step] = request.cls.teststeps

    logger.debug(f"setup testcase fixture: {config.name} - {request.module.__name__}")

    def update_request_headers(steps, index):
        for teststep in steps:
            if teststep.request:
                index += 1
                teststep.request.headers["X-Request-ID"] = f"{prefix}-{index}"
            elif teststep.testcase and hasattr(teststep.testcase, "teststeps"):
                update_request_headers(teststep.testcase.teststeps, index)

    # you can update testcase teststep like this
    prefix = f"HRUN-{uuid.uuid4()}"
    update_request_headers(teststeps, 0)

    yield

    logger.debug(
        f"teardown testcase fixture: {config.name} - {request.module.__name__}"
    )

    summary = request.instance.get_summary()
    logger.debug(f"testcase result summary: {summary}")

    # TODO: upload testcase summary
