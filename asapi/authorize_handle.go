package asapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/antlinker/go-cache"
	"github.com/astaxie/beego/httplib"
)

// NewAuthorizeHandle 创建授权处理
func NewAuthorizeHandle(cfg *Config) *AuthorizeHandle {
	ah := &AuthorizeHandle{
		cfg: cfg,
		th:  NewTokenHandle(cfg),
	}

	if ah.cfg.IsEnabledCache {
		if ah.cfg.CacheGCInterval == 0 {
			ah.cfg.CacheGCInterval = 300
		}
		ah.cache = cache.New(0, time.Second*time.Duration(ah.cfg.CacheGCInterval))
		// 加入两个接口缓存
		ah.routerCache = cache.New(0, time.Second*time.Duration(ah.cfg.CacheGCInterval))
	}

	return ah
}

// AuthorizeHandle 授权处理
type AuthorizeHandle struct {
	cfg         *Config
	th          *TokenHandle
	cache       *cache.Cache
	routerCache *cache.Cache
}

// getFromRouterCache 从路由的缓存中读数据
func (ah *AuthorizeHandle) getFromRouterCache(router string, r RequestReader) (b []byte, ok bool) {
	if !ah.cfg.IsEnabledCache {
		return
	}
	if r.Expires(router) <= 0 {
		ok = false
		return
	}
	key := r.Hash()
	if key == "" {
		return
	}

	// 检查缓存数据
	v, ok := ah.routerCache.Get(key)
	if !ok || v == nil {
		return
	}
	b, ok = v.([]byte)
	return
}

func (ah *AuthorizeHandle) setRouterCache(router string, r RequestReader, v interface{}) {
	if !ah.cfg.IsEnabledCache {
		return
	}
	expires := r.Expires(router)
	if expires <= 0 {
		return
	}
	key := r.Hash()
	if key == "" {
		return
	}
	b, _ := json.Marshal(v)
	ah.routerCache.Set(key, b, time.Duration(expires)*time.Second)
}

// 请求数据
func (ah *AuthorizeHandle) request(router, method string, reqHandle func(req *httplib.BeegoHTTPRequest) (*httplib.BeegoHTTPRequest, *ErrorResult), v interface{}) (result *ErrorResult) {
	req := httplib.NewBeegoRequest(ah.cfg.GetURL(router), method)

	if reqHandle != nil {
		vreq, vresult := reqHandle(req)
		if vresult != nil {
			result = vresult
			return
		}
		req = vreq
	}

	res, err := req.Response()
	if err != nil {
		result = NewErrorResult(err.Error())
		return
	}

	buf, err := req.Bytes()
	if err != nil {
		result = NewErrorResult(err.Error())
		return
	}

	switch res.StatusCode {
	case 200:
		if v == nil {
			return
		}
		err = json.Unmarshal(buf, v)
		if err != nil {
			result = NewErrorResult(err.Error())
			return
		} // 设置缓存
	default:
		result = NewErrorResult(string(buf), res.StatusCode)
	}

	return
}

// 带有访问令牌的post请求
func (ah *AuthorizeHandle) tokenPost(router string, body, v interface{}) (result *ErrorResult) {
	// 从缓存读取
	reader, shouldCached := body.(RequestReader)
	if shouldCached {
		b, exists := ah.getFromRouterCache(router, reader)
		if exists {
			if len(b) == 0 {
				return
			}
			if v == nil {
				return
			}
			if err := json.Unmarshal(b, v); err != nil {
				result = NewErrorResult(err.Error())
			}
			// println(router, "cached")
			return
		}
	}

	reqHandle := func(req *httplib.BeegoHTTPRequest) (*httplib.BeegoHTTPRequest, *ErrorResult) {
		token, result := ah.th.Get()
		if result != nil {
			return req, result
		}
		req = req.Header("AccessToken", token)

		if body != nil {
			vreq, err := req.JSONBody(body)
			if err != nil {
				result = NewErrorResult(err.Error())
				return req, result
			}
			req = vreq
		}
		return req, nil
	}
	result = ah.request(router, http.MethodPost, reqHandle, v)
	if result != nil {
		return
	}
	if shouldCached {
		ah.setRouterCache(router, reader, v)
	}
	return
}

// GetConfig 获取配置参数
func (ah *AuthorizeHandle) GetConfig() (cfg *Config) {
	cfg = ah.cfg
	return
}

// LoginUserInfo 登录用户信息
type LoginUserInfo struct {
	MobilePhone     string // 手机号码
	UserCode        string // 用户代码
	IDCard          string // 身份证号码
	Password        string // 登录密码
	DefaultPassword string // 默认登录密码
	University      string // 学校ID
	UserType        string // 用户类型
}

// VerifyLogin 验证登录
// username 用户ID（唯一标识）
// password 密码
func (ah *AuthorizeHandle) VerifyLogin(username, password string) (info *LoginUserInfo, result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             username,
		"Password":        password,
	}
	var loginInfo LoginUserInfo
	result = ah.tokenPost("/api/authorize/verifylogin", body, &loginInfo)
	if result != nil {
		return
	}
	info = &loginInfo
	return
}

// GetUser 验证登录
// uid 用户ID（唯一标识）
func (ah *AuthorizeHandle) GetUser(uid string) (info *LoginUserInfo, result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             uid,
	}
	var loginInfo LoginUserInfo
	result = ah.tokenPost("/api/authorize/getuser", body, &loginInfo)
	if result != nil {
		return
	}
	info = &loginInfo
	return
}

// AuthorizeAddUserRequest 增加用户信息请求
type AuthorizeAddUserRequest struct {
	MobilePhone     string
	UserCode        string
	IDCard          string
	Password        string
	DefaultPassword string
	University      string
	ServiceIdentify string
}

// AddUser 增加用户
func (ah *AuthorizeHandle) AddUser(uid string, user *AuthorizeAddUserRequest) (result *ErrorResult) {
	identify := ah.cfg.ServiceIdentify

	if v := user.ServiceIdentify; v != "" {
		identify = v
	}

	body := map[string]interface{}{
		"ServiceIdentify": identify,
		"UID":             uid,
		"MobilePhone":     user.MobilePhone,
		"UserCode":        user.UserCode,
		"IDCard":          user.IDCard,
		"Password":        user.Password,
		"DefaultPassword": user.DefaultPassword,
		"University":      user.University,
	}
	result = ah.tokenPost("/api/authorize/adduser", body, nil)
	return
}

// AuthorizeEditUserRequest 编辑用户信息请求
type AuthorizeEditUserRequest struct {
	MobilePhone     string
	UserCode        string
	IDCard          string
	University      string
	ServiceIdentify string
}

// EditUser 编辑用户信息
func (ah *AuthorizeHandle) EditUser(uid string, user *AuthorizeEditUserRequest) (result *ErrorResult) {
	identify := ah.cfg.ServiceIdentify

	if v := user.ServiceIdentify; v != "" {
		identify = v
	}
	body := map[string]interface{}{
		"ServiceIdentify": identify,
		"UID":             uid,
		"MobilePhone":     user.MobilePhone,
		"UserCode":        user.UserCode,
		"IDCard":          user.IDCard,
		"University":      user.University,
	}
	result = ah.tokenPost("/api/authorize/edituser", body, nil)
	return
}

// DelUser 删除用户
func (ah *AuthorizeHandle) DelUser(uid string) (result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             uid,
	}
	result = ah.tokenPost("/api/authorize/deluser", body, nil)
	return
}

// ModifyPwd 修改密码
func (ah *AuthorizeHandle) ModifyPwd(uid, password string, services ...string) (result *ErrorResult) {
	identify := ah.cfg.ServiceIdentify
	if len(services) > 0 {
		identify = services[0]
	}
	body := map[string]interface{}{
		"ServiceIdentify": identify,
		"UID":             uid,
		"Password":        password,
	}
	result = ah.tokenPost("/api/authorize/modifypwd", body, nil)
	return
}

// CheckDefaultPwd 检查默认密码
func (ah *AuthorizeHandle) CheckDefaultPwd(uid string) (isDefault bool, result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             uid,
	}

	var res struct {
		IsDefault bool
	}
	result = ah.tokenPost("/api/authorize/checkdefaultpwd", body, &res)
	if result != nil {
		return
	}
	isDefault = res.IsDefault
	return
}

// GetToken 获取访问令牌
func (ah *AuthorizeHandle) GetToken() (token string, result *ErrorResult) {
	token, result = ah.th.Get()
	return
}

// ForceGetToken 强制获取访问令牌
func (ah *AuthorizeHandle) ForceGetToken() (tokenString string, result *ErrorResult) {
	token, result := ah.th.ForceGet()
	if result != nil {
		return
	}
	tokenString = token.AccessToken
	return
}

// VerifyToken 验证令牌
func (ah *AuthorizeHandle) VerifyToken(token string) (userID, clientID string, result *ErrorResult) {
	const (
		userIDKey   = "UserID"
		clientIDKey = "ClientID"
	)

	if ah.cfg.IsEnabledCache {
		// 检查缓存数据
		if at, ok := ah.cache.Get(token); ok {
			if atm, ok := at.(map[string]string); ok {
				userID = atm[userIDKey]
				clientID = atm[clientIDKey]
				return
			}
		}
	}

	reqHandle := func(req *httplib.BeegoHTTPRequest) (*httplib.BeegoHTTPRequest, *ErrorResult) {
		req = req.Param("access_token", token)
		req = req.Param("service", ah.GetConfig().ServiceIdentify)
		return req, nil
	}

	var resData struct {
		UserID    string `json:"user_id"`
		ClientID  string `json:"client_id"`
		ExpiresIn int    `json:"expires_in"`
	}

	result = ah.request("/oauth2/verify", http.MethodGet, reqHandle, &resData)
	if result != nil {
		return
	}

	userID = resData.UserID
	clientID = resData.ClientID

	if ah.cfg.IsEnabledCache && ah.cfg.CacheGCInterval < resData.ExpiresIn {
		data := map[string]string{
			userIDKey:   resData.UserID,
			clientIDKey: resData.ClientID,
		}
		ah.cache.Set(token, data, time.Duration(resData.ExpiresIn-ah.cfg.CacheGCInterval)*time.Second)
	}

	return
}

// VerifyTokenInfo 验证令牌的响应
type VerifyTokenInfo struct {
	UserID      string `json:"user_id"`
	BusinessID  string `json:"business_id"`
	UserCode    string `json:"user_code"`
	ClientID    string `json:"client_id"`
	ExpiresIn   int    `json:"expires_in"`
	ServiceCode string `json:"service_code"`
	ServiceAddr string `json:"service_addr"`
}

// VerifyTokenV2 验证令牌
func (ah *AuthorizeHandle) VerifyTokenV2(token string) (*VerifyTokenInfo, *ErrorResult) {
	if ah.cfg.IsEnabledCache {
		// 检查缓存数据
		if at, ok := ah.cache.Get(token); ok {
			if atm, ok := at.(*VerifyTokenInfo); ok {
				return atm, nil
			}
		}
	}
	reqHandle := func(req *httplib.BeegoHTTPRequest) (*httplib.BeegoHTTPRequest, *ErrorResult) {
		req = req.Param("access_token", token)
		req = req.Param("service", ah.GetConfig().ServiceIdentify)
		return req, nil
	}

	var resData VerifyTokenInfo
	if result := ah.request("/oauth2/verify/v2",
		http.MethodGet, reqHandle, &resData); result != nil {
		return nil, result
	}
	if ah.cfg.IsEnabledCache && ah.cfg.CacheGCInterval < resData.ExpiresIn {
		ah.cache.Set(token, &resData, time.Duration(resData.ExpiresIn-ah.cfg.CacheGCInterval)*time.Second)
	}
	return &resData, nil
}

// GetUpgradeToken 获取升级令牌
func (ah *AuthorizeHandle) GetUpgradeToken(password, uid, clientID, clientSecret string) (info map[string]interface{}, result *ErrorResult) {

	reqHandle := func(req *httplib.BeegoHTTPRequest) (*httplib.BeegoHTTPRequest, *ErrorResult) {
		req = req.SetBasicAuth(clientID, clientSecret)

		req = req.Param("grant_type", "password")
		userInfo := map[string]interface{}{
			"LoginModel":   9,
			"UserName":     uid,
			"ClientID":     ah.GetConfig().ClientID,
			"ClientSecret": ah.GetConfig().ClientSecret,
		}

		buf, _ := json.Marshal(userInfo)
		userName := base64.StdEncoding.EncodeToString(buf)
		req = req.Param("username", userName)
		req = req.Param("password", password)

		return req, nil
	}

	result = ah.request("/oauth2/token", http.MethodPost, reqHandle, &info)

	return
}

// UserTokenInfo 用户令牌信息
type UserTokenInfo struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Expires      int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	UserID       string `json:"user_id"`
}

// UserLoginToken 用户登录令牌
func (ah *AuthorizeHandle) UserLoginToken(userName, password, service string) (*UserTokenInfo, *ErrorResult) {
	return ah.GetAccessTokenByPassword(PasswordRequest{
		ClientID:     ah.GetConfig().ClientID,
		ClientSecret: ah.GetConfig().ClientSecret,
		LoginModel:   1,
		UserName:     userName,
		Service:      service,
		Password:     password,
	})
}

// PasswordRequest 密码模式请求参数
type PasswordRequest struct {
	ClientID     string // 客户端ID
	ClientSecret string // 客户端秘钥
	LoginModel   int    // 登录模式（1手机号、身份证号登录,2学校、学号登录）
	University   string // 学校编号
	UserName     string // 用户名
	Service      string // 服务标识
	Password     string // 密码
}

// GetAccessTokenByPassword 使用密码模式获取访问令牌
func (ah *AuthorizeHandle) GetAccessTokenByPassword(params PasswordRequest) (*UserTokenInfo, *ErrorResult) {
	reqHandle := func(req *httplib.BeegoHTTPRequest) (*httplib.BeegoHTTPRequest, *ErrorResult) {
		req = req.SetBasicAuth(params.ClientID, params.ClientSecret)
		req = req.Param("grant_type", "password")

		info := map[string]interface{}{
			"Service":    params.Service,
			"LoginModel": params.LoginModel,
			"UserName":   params.UserName,
			"University": params.University,
		}
		buf, err := json.Marshal(info)
		if err != nil {
			return nil, NewErrorResult(err.Error())
		}

		userName := base64.StdEncoding.EncodeToString(buf)
		req = req.Param("username", userName)
		req = req.Param("password", params.Password)

		return req, nil
	}

	var info UserTokenInfo
	result := ah.request("/oauth2/token", http.MethodPost, reqHandle, &info)
	if result != nil {
		return nil, result
	}

	return &info, nil
}

// UserRefreshToken 用户更新令牌
func (ah *AuthorizeHandle) UserRefreshToken(rtoken string) (tokenInfo *UserTokenInfo, result *ErrorResult) {

	reqHandle := func(req *httplib.BeegoHTTPRequest) (*httplib.BeegoHTTPRequest, *ErrorResult) {
		req = req.SetBasicAuth(ah.GetConfig().ClientID, ah.GetConfig().ClientSecret)
		req = req.Param("grant_type", "refresh_token")
		req = req.Param("refresh_token", rtoken)

		return req, nil
	}

	var info UserTokenInfo
	result = ah.request("/oauth2/token", http.MethodPost, reqHandle, &info)
	if result != nil {
		return
	}
	tokenInfo = &info

	return
}

// AuthorizeMergeUserRequest 合并用户请求参数
type AuthorizeMergeUserRequest struct {
	UID         string
	TUID        string
	TUserCode   string
	TUniversity string
}

// MergeUser 合并用户
func (ah *AuthorizeHandle) MergeUser(req *AuthorizeMergeUserRequest) (result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             req.UID,
		"TUID":            req.TUID,
		"TUserCode":       req.TUserCode,
		"TUniversity":     req.TUniversity,
	}

	result = ah.tokenPost("/api/authorize/mergeuser", body, nil)
	return
}

// GetStaffParam 获取学工请求参数
func (ah *AuthorizeHandle) GetStaffParam(identify, uid string) (buID, addr string, result *ErrorResult) {
	body := &GetStaffParamRequest{
		ServiceIdentify: identify,
		UID:             uid,
	}

	var resData struct {
		BuID string
		Addr string
	}

	result = ah.tokenPost("/api/authorize/getstaffparam", body, &resData)
	if result != nil {
		return
	}

	buID = resData.BuID
	addr = resData.Addr

	return
}

// GetAntStaffParamResult 获取ANT用户学工参数查询结果
type GetAntStaffParamResult struct {
	BuID          string
	Addr          string
	University    string
	IntelUserCode string
}

// GetAntStaffParam 获取ANT用户学工参数
func (ah *AuthorizeHandle) GetAntStaffParam(uid string) (*GetAntStaffParamResult, *ErrorResult) {
	body := &GetStaffParamRequest{
		ServiceIdentify: "ANT",
		UID:             uid,
	}

	var resData GetAntStaffParamResult
	result := ah.tokenPost("/api/authorize/getstaffparam", body, &resData)
	if result != nil {
		return nil, result
	}
	return &resData, nil
}

// AuthorizeMergeTELUserRequest 合并手机号用户请求参数
type AuthorizeMergeTELUserRequest struct {
	MUID string
	CUID string
}

// MergeTELUser 合并手机号用户
func (ah *AuthorizeHandle) MergeTELUser(req *AuthorizeMergeTELUserRequest) (result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"MUID":            req.MUID,
		"CUID":            req.CUID,
	}

	result = ah.tokenPost("/api/authorize/mergeteluser", body, nil)
	return
}

// ClearAuthRequest 清理用户认证信息请求参数
type ClearAuthRequest struct {
	UID        string
	University string
}

// ClearAuth 清理用户认证信息
func (ah *AuthorizeHandle) ClearAuth(req *ClearAuthRequest) (result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             req.UID,
		"University":      req.University,
	}

	result = ah.tokenPost("/api/authorize/clearauth", body, nil)
	return
}

// GetUserCode 根据用户ID获取UserCode
func (ah *AuthorizeHandle) GetUserCode(uid string) (userCode string, result *ErrorResult) {
	body := &GetUserCodeRequest{
		UID: uid,
	}

	var res struct {
		UserCode string
	}

	result = ah.tokenPost("/api/authorize/usercode", body, &res)
	if result != nil {
		return
	}
	userCode = res.UserCode
	return
}

// AddStaffUserRequest 增加学工用户请求参数
type AddStaffUserRequest struct {
	UID         string // 用户标识
	MobilePhone string // 手机号
	UserCode    string // 学号
	IDCard      string // 身份证号
	Password    string // 密码
	University  string // 学校ID
	Name        string // 真实姓名
	Sex         string // 性别（F女,M男）
	DeptID      string // 部门或学院ID
}

// AddStaffUser 增加学工用户
func (ah *AuthorizeHandle) AddStaffUser(req *AddStaffUserRequest) (result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             req.UID,
		"MobilePhone":     req.MobilePhone,
		"UserCode":        req.UserCode,
		"IDCard":          req.IDCard,
		"Password":        req.Password,
		"University":      req.University,
		"Name":            req.Name,
		"Sex":             req.Sex,
		"DeptID":          req.DeptID,
	}

	result = ah.tokenPost("/api/authorize/addstaffuser", body, nil)
	return
}

// UpdateUserBasicRequest 更新用户基础信息请求参数
type UpdateUserBasicRequest struct {
	UID    string // 用户标识
	Name   string // 真实姓名
	DeptID string // 部门或学院ID
}

// UpdateUserBasic 更新用户基础信息
func (ah *AuthorizeHandle) UpdateUserBasic(req *UpdateUserBasicRequest) (result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             req.UID,
		"Name":            req.Name,
		"DeptID":          req.DeptID,
	}

	result = ah.tokenPost("/api/authorize/updateuserbasic", body, nil)
	return
}

// GetUserVersionResult 用户版本信息
type GetUserVersionResult struct {
	ClearAuth int // 清理用户认证信息(0不清理 1清理)
	Version   int // 版本号
	Activate  int // 激活状态（0已激活，1未激活）
}

// GetUserVersion 获取用户版本信息
func (ah *AuthorizeHandle) GetUserVersion(uid string) (resResult *GetUserVersionResult, result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             uid,
	}

	var res GetUserVersionResult

	result = ah.tokenPost("/api/authorize/getuserversion", body, &res)
	if result != nil {
		return
	}
	resResult = &res
	return
}

// UserActivateResult 用户激活
type UserActivateResult struct {
	MobilePhone string // 手机号码
	UserCode    string // 用户代码
	IDCard      string // 身份证号码
	University  string // 学校ID
	RealName    string // 真实姓名
	Sex         string // 性别（F女,M男）
	DeptID      string // 部门ID(学工是学院或部门，学生是班级)
	UserType    string // 用户类型（1学生 2老师）
}

// UserActivate 用户激活
func (ah *AuthorizeHandle) UserActivate(uid string) (resResult *UserActivateResult, result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             uid,
	}

	var res UserActivateResult

	result = ah.tokenPost("/api/authorize/useractivate", body, &res)
	if result != nil {
		return
	}
	resResult = &res
	return
}

// GetUserUpdateResult 获取用户更新信息
type GetUserUpdateResult struct {
	RealName string // 真实姓名
	DeptID   string // 部门ID(学工是学院或部门，学生是班级)
}

// GetUserUpdate 获取获取用户更新信息
func (ah *AuthorizeHandle) GetUserUpdate(uid string) (resResult *GetUserUpdateResult, result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             uid,
	}

	var res GetUserUpdateResult

	result = ah.tokenPost("/api/authorize/getuserupdate", body, &res)
	if result != nil {
		return
	}
	resResult = &res
	return
}

// DelStaffUser 删除学工用户
func (ah *AuthorizeHandle) DelStaffUser(uid string) (result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             uid,
	}

	result = ah.tokenPost("/api/authorize/delstaffuser", body, nil)
	return
}

// UpdateAuthStatus 更新用户认证状态
func (ah *AuthorizeHandle) UpdateAuthStatus(uid string) (result *ErrorResult) {
	body := map[string]interface{}{
		"ServiceIdentify": ah.cfg.ServiceIdentify,
		"UID":             uid,
	}

	result = ah.tokenPost("/api/authorize/updateauthstatus", body, nil)
	return
}

// GetAntUIDList 获取ANT用户ID列表
func (ah *AuthorizeHandle) GetAntUIDList(service string, uids ...string) (auids []string, result *ErrorResult) {
	svc := ah.cfg.ServiceIdentify
	if service != "" {
		svc = service
	}

	body := map[string]interface{}{
		"ServiceIdentify": svc,
		"UID":             uids,
	}

	var res struct {
		ANTUID []string
	}

	result = ah.tokenPost("/api/authorize/getantuser", body, &res)
	if result != nil {
		return
	}
	auids = res.ANTUID

	return
}

// GetAntUIDByUniversity 根据学校查询ANT用户ID
func (ah *AuthorizeHandle) GetAntUIDByUniversity(userID, university string) (uid string, result *ErrorResult) {
	body := &GetAntUIDByUniversityRequest{
		ServiceIdentify: ah.cfg.ServiceIdentify,
		UserID:          userID,
		University:      university,
	}

	var res struct {
		UID string
	}

	result = ah.tokenPost("/api/authorize/antuidbyuniversity", body, &res)
	if result != nil {
		return
	}
	uid = res.UID

	return
}
