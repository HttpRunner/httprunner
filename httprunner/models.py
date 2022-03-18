import os
from enum import Enum
from typing import Dict, Text, Union, Callable, Any, List, Tuple

from pydantic import BaseModel, Field
from pydantic import HttpUrl

Name = Text
Url = Text
BaseUrl = Union[HttpUrl, Text]
VariablesMapping = Dict[Text, Any]
FunctionsMapping = Dict[Text, Callable]
Headers = Dict[Text, Text]
Cookies = Dict[Text, Text]
Verify = bool
Hooks = List[Union[Text, Dict[Text, Text]]]
Export = List[Text]
Validators = List[Dict]
Env = Dict[Text, Any]


class MethodEnum(Text, Enum):
    GET = "GET"
    POST = "POST"
    PUT = "PUT"
    DELETE = "DELETE"
    HEAD = "HEAD"
    OPTIONS = "OPTIONS"
    PATCH = "PATCH"


class TRetry(BaseModel):
    tries: int
    delay: int


class SkipIF(BaseModel):
    condition: Text = None
    reason: Text = None


class XFail(BaseModel):
    condition: Text = None
    reason: Text = None
    raises: Any = None                   # expect type: None or Exception
    run: bool = True
    strict: bool = False


class TConfig(BaseModel):
    name: Name
    verify: Verify = False
    base_url: BaseUrl = ""
    # Text: prepare variables in debugtalk.py, ${gen_variables()}
    variables: Union[VariablesMapping, Text] = {}
    # setup_hooks: Hooks = []
    # teardown_hooks: Hooks = []
    export: Export = []
    path: Text = None
    weight: int = 1

    filterwarnings: Text = None
    parameters: Union[VariablesMapping, Text] = {}
    usefixtures: List[Text] = []
    skip: Text = None
    skipif: SkipIF = None
    custom_marks: List[Text] = []                                   # allure tags
    xfail: XFail = None

    description: Text = ""                                          # allure description
    links: List[Dict] = []                                          # allure links


class TRequest(BaseModel):
    """requests.Request model"""

    method: MethodEnum
    url: Url
    params: Dict[Text, Text] = {}
    headers: Headers = {}
    req_json: Union[Dict, List, Text] = Field(None, alias="json")
    data: Union[Text, Dict[Text, Any]] = None
    cookies: Cookies = {}
    timeout: float = 120
    allow_redirects: bool = True
    verify: Verify = False
    upload: Dict = {}  # used for upload files


class TStep(BaseModel):
    name: Name
    request: Union[TRequest, None] = None
    testcase: Union[Text, Callable, None] = None
    variables: VariablesMapping = {}
    setup_hooks: Hooks = []
    teardown_hooks: Hooks = []
    # used to extract request's response field
    extract: VariablesMapping = {}
    # used to export session variables from referenced testcase
    export: Export = []
    validators: Validators = Field([], alias="validate")
    validate_script: List[Text] = []
    retry: Union[TRetry, None] = TRetry(tries=1, delay=0)


class TestCase(BaseModel):
    config: TConfig
    teststeps: List[TStep]


class ProjectMeta(BaseModel):
    debugtalk_py: Text = ""  # debugtalk.py file content
    debugtalk_path: Text = ""  # debugtalk.py file path
    dot_env_path: Text = ""  # .env file path
    functions: FunctionsMapping = {}  # functions defined in debugtalk.py
    env: Env = {}
    RootDir: Text = os.getcwd()  # project root directory (ensure absolute), the path debugtalk.py located


class TestsMapping(BaseModel):
    project_meta: ProjectMeta
    testcases: List[TestCase]


class TestCaseTime(BaseModel):
    start_at: float = 0
    start_at_iso_format: Text = ""
    duration: float = 0


class TestCaseInOut(BaseModel):
    config_vars: VariablesMapping = {}
    export_vars: Dict = {}


class RequestStat(BaseModel):
    content_size: float = 0
    response_time_ms: float = 0
    elapsed_ms: float = 0


class AddressData(BaseModel):
    client_ip: Text = "N/A"
    client_port: int = 0
    server_ip: Text = "N/A"
    server_port: int = 0


class RequestData(BaseModel):
    method: MethodEnum = MethodEnum.GET
    url: Url
    headers: Headers = {}
    cookies: Cookies = {}
    body: Union[Text, bytes, List, Dict, None] = {}


class ResponseData(BaseModel):
    status_code: int
    headers: Dict
    cookies: Cookies
    encoding: Union[Text, None] = None
    content_type: Text
    body: Union[Text, bytes, List, Dict]


class ReqRespData(BaseModel):
    request: RequestData
    response: ResponseData


class SessionData(BaseModel):
    """request session data, including request, response, validators and stat data"""

    success: bool = False
    # in most cases, req_resps only contains one request & response
    # while when 30X redirect occurs, req_resps will contain multiple request & response
    req_resps: List[ReqRespData] = []
    stat: RequestStat = RequestStat()
    address: AddressData = AddressData()
    validators: Dict = {}


class StepData(BaseModel):
    """teststep data, each step maybe corresponding to one request or one testcase"""

    success: bool = False
    name: Text = ""  # teststep name
    data: Union[SessionData, List['StepData']] = None
    export_vars: VariablesMapping = {}

        
StepData.update_forward_refs()


class TestCaseSummary(BaseModel):
    name: Text
    success: bool
    case_id: Text
    time: TestCaseTime
    in_out: TestCaseInOut = {}
    log: Text = ""
    step_datas: List[StepData] = []


class PlatformInfo(BaseModel):
    httprunner_version: Text
    python_version: Text
    platform: Text


class TestCaseRef(BaseModel):
    name: Text
    base_url: Text = ""
    testcase: Text
    variables: VariablesMapping = {}


class TestSuite(BaseModel):
    config: TConfig
    testcases: List[TestCaseRef]


class Stat(BaseModel):
    total: int = 0
    success: int = 0
    fail: int = 0


class TestSuiteSummary(BaseModel):
    success: bool = False
    stat: Stat = Stat()
    time: TestCaseTime = TestCaseTime()
    platform: PlatformInfo
    testcases: List[TestCaseSummary]
