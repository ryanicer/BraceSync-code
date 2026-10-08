// Package handler — T625 盲区用例：技师手机号写侧三态（T624）+ 新建技师的角色面（T623）
//
// 本文件是**新增**测试文件，不改动 handler_impl_test.go（T030 实现侧，含本包唯一
// fakeStore/testEnv 夹具）与 technician_password_t480_test.go 等既有用例。
// 复用夹具：newEnv（handler_impl_test.go:750）、testEnv.do（:784）、testPhoneKey（:33）、
// fakeStore 的技师侧字段 tech/updatedTech/createdTech/phoneTaken/lastTechInput/lastTechQuery（:82-96、:281）。
//
// 被测实现（现读，user-service/internal/handler/handler.go）：
//   - updateTechnician :1191（PUT /api/v1/admin/technicians/:techId，路由 :272）
//   - createTechnician   :1135（POST /api/v1/admin/technicians，路由 :271）
//   - validPhone         :1088（11 位、首位 1、逐字符数字）
//   - preparePhone       :1108（AES-GCM 密文 + SHA-256 哈希；未配密钥 500）
//   - phone 语义          :1221-1242（key 缺席/空串 = 沿用原密文与原哈希；非空 = 校验→查重→换新）
//   - techRequest         :1081（只有 name/phone/teamId 三键，无角色键）
package handler

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t625OldPhone = "13900002222" // 库内既有号码（编辑前的原值）
	t625NewPhone = "13700003333" // 合法新号
)

// t625TechEnv 装配一个「技师 T1 已存在、团队存在、密钥已配」的编辑现场，返回环境与原行。
// updatedTech 默认指向 existing：模拟 repo 的 UPDATE ... RETURNING 把入参原样落回并可读。
func t625TechEnv(t *testing.T) (*testEnv, *repo.TechnicianRow) {
	t.Helper()
	e := newEnv(t, true, true)
	cipher, err := phone.NewCipher(testPhoneKey)
	require.NoError(t, err)
	enc, err := cipher.Encrypt(t625OldPhone)
	require.NoError(t, err)

	existing := &repo.TechnicianRow{
		TechID: "T1", Name: "旧名", PhoneEnc: enc, PhoneHash: phone.Hash(t625OldPhone),
		TeamID: strPtr("TEAM01"), Status: "enabled", AuthStatus: "authorized",
	}
	e.store.tech = existing
	e.store.updatedTech = existing
	e.store.teamExists = true
	return e, existing
}

// ─────────────────────────────────────────────────────────────
// 1) 空 phone = 保留原值：既不覆盖为空也不报错（T624 的关键退化面）
// ─────────────────────────────────────────────────────────────

func TestT625_UpdateTechnician_EmptyPhoneKeepsStoredNumber(t *testing.T) {
	t.Run("显式空串", func(t *testing.T) {
		e, existing := t625TechEnv(t)

		w, resp := e.do(http.MethodPut, "/api/v1/admin/technicians/T1",
			map[string]any{"name": "只改名", "phone": ""}, nil)
		require.Equal(t, http.StatusOK, w.Code, resp.Message)

		// 落库入参里的密文/哈希仍是原值（handler.go:1221 先取 existing，再只在 req.Phone != "" 时替换）
		assert.Equal(t, "只改名", e.store.lastTechInput.Name)
		assert.Equal(t, existing.PhoneHash, e.store.lastTechInput.PhoneHash,
			"空 phone 不得改动 phone_hash：改动会让该技师的登录手机号支失效（T487 双凭证）")
		assert.Equal(t, existing.PhoneEnc, e.store.lastTechInput.PhoneEnc, "空 phone 不得改动 phone_enc")
		assert.NotEmpty(t, e.store.lastTechInput.PhoneEnc, "原密文不能被洗成零长")

		// 号码查重一次都不该发生：沿用原号无需判断「是否被别人占」
		cipher, err := phone.NewCipher(testPhoneKey)
		require.NoError(t, err)
		plain, err := cipher.Decrypt(e.store.lastTechInput.PhoneEnc)
		require.NoError(t, err)
		assert.Equal(t, t625OldPhone, plain, "回读原密文得到原号码")
	})

	t.Run("phone 键缺席", func(t *testing.T) {
		e, existing := t625TechEnv(t)

		w, _ := e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"name": "键缺席"}, nil)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, existing.PhoneHash, e.store.lastTechInput.PhoneHash)
		assert.Equal(t, existing.PhoneEnc, e.store.lastTechInput.PhoneEnc)
	})

	t.Run("空白姓名沿用原值且不改号", func(t *testing.T) {
		e, existing := t625TechEnv(t)
		w, _ := e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"name": "   "}, nil)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, existing.Name, e.store.lastTechInput.Name, "空白姓名沿用原值（handler.go:1209）")
		assert.Equal(t, existing.PhoneHash, e.store.lastTechInput.PhoneHash)
	})

	t.Run("空串不得触发查重或 500", func(t *testing.T) {
		e, _ := t625TechEnv(t)
		e.store.phoneTaken = true // 若空串被当成「新号」去查重，这里就会 409
		w, _ := e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"phone": ""}, nil)
		assert.Equal(t, http.StatusOK, w.Code, "空 phone 不进入 validPhone/preparePhone/TechPhoneHashTaken")
	})

	t.Run("纯空白串按非法号处理（不是按空处理）", func(t *testing.T) {
		// 现读实现只把 len(req.Phone)==0 当「不改号」（handler.go:1222），"   " 走 validPhone 判非 11 位 ⇒ 400。
		// 前端不会把这串发下来（apps/admin-web/src/utils/phoneField.ts:47 phonePatch 先 trim、空则整键不下发），
		// 所以这条只是服务端兜底面：宁可 400 也不能把空白当空号写进 phone_enc。
		e, existing := t625TechEnv(t)
		e.store.lastTechInput = repo.TechInput{}

		w, resp := e.do(http.MethodPut, "/api/v1/admin/technicians/T1",
			map[string]any{"name": "  ", "phone": "   "}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code, "空白 phone 现口径 = 拒绝，不是沿用原号")
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
		assert.Empty(t, e.store.lastTechInput.Name, "被拒后不得写库")
		assert.Equal(t, existing.PhoneHash, e.store.tech.PhoneHash)
	})
}

// ─────────────────────────────────────────────────────────────
// 2) 非 11 位 / 首位非 1 的 phone 被拒（handler.go:1223 → model.ErrInvalidParam → 400/1xxxx）
// ─────────────────────────────────────────────────────────────

func TestT625_UpdateTechnician_RejectsMalformedPhone(t *testing.T) {
	bad := []struct {
		name  string
		phone string
	}{
		{"十位", "1380000111"},
		{"十二位", "138000011112"},
		{"首位非 1", "23800001111"},
		{"全零开头", "03800001111"},
		{"含字母", "1380000ab11"},
		{"含空格", "138 00001111"},
		{"含加号国际前缀", "+8613800001111"},
		{"带连字符", "138-0000111"},
		{"纯符号", "###########"},
		{"十一位但首位 9", "93800001111"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			e, existing := t625TechEnv(t)
			e.store.phoneTaken = true // 非法号必须在校验那格就被拦下，轮不到查重
			e.store.lastTechInput = repo.TechInput{}

			w, resp := e.do(http.MethodPut, "/api/v1/admin/technicians/T1",
				map[string]any{"name": "顺手改号", "phone": tc.phone}, nil)
			assert.Equal(t, http.StatusBadRequest, w.Code, "phone=%q 应 400", tc.phone)
			assert.Equal(t, model.CodeInvalidParam, resp.Code)
			// 出参文案是面向运营的兜底串（不含字段名），号码本身绝不能出现在响应里
			assert.NotContains(t, w.Body.String(), tc.phone, "被拒的号码不得回显")

			// 被拒的写不得触达存储：lastTechInput 仍是零值（UpdateTechnician 未被调用）
			assert.Empty(t, e.store.lastTechInput.Name, "非法号不得落库")
			assert.Empty(t, e.store.lastTechInput.PhoneEnc, "非法号不得落库")
			assert.Equal(t, existing.PhoneHash, e.store.tech.PhoneHash, "对照：库内原哈希未动")
		})
	}

	// 合法号反向自检：同一条路径换个合法号码就该 200，证明上面的 400 来自号码形状而非用例装配
	e, _ := t625TechEnv(t)
	w, _ := e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"phone": t625NewPhone}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ─────────────────────────────────────────────────────────────
// 3) 合法新 phone：保存后可读回（在 fake 能观察的面上钉：落库入参 + 回显脱敏形状）
// ─────────────────────────────────────────────────────────────

func TestT625_UpdateTechnician_ValidPhoneSavedAndReadableBack(t *testing.T) {
	e, _ := t625TechEnv(t)
	cipher, err := phone.NewCipher(testPhoneKey)
	require.NoError(t, err)
	newEnc, err := cipher.Encrypt(t625NewPhone)
	require.NoError(t, err)

	w, resp := e.do(http.MethodPut, "/api/v1/admin/technicians/T1",
		map[string]any{"name": "旧名", "phone": t625NewPhone}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	// 写侧：新密文可解回新号、新哈希 = SHA-256(规范化号)，两者成套换掉旧的
	in := e.store.lastTechInput
	assert.NotEqual(t, phone.Hash(t625OldPhone), in.PhoneHash, "原哈希必须被替换")
	assert.Equal(t, phone.Hash(t625NewPhone), in.PhoneHash)
	plain, err := cipher.Decrypt(in.PhoneEnc)
	require.NoError(t, err)
	assert.Equal(t, t625NewPhone, plain, "密文回读 = 新号（加密与哈希不能错开一位）")

	// 查重是以「排除自己」的口径发起的：techID 作为排除参数传给 store（handler.go:1232）。
	// fakeStore 不记录该参数（不改动它），故这里只钉「查过重、且没被判重」这一面。
	assert.Equal(t, "T1", e.store.lastTechQuery, "写前先按 :techId 读原行")

	// 读侧：让 fake 的 RETURNING 行带回新密文，回显必须是脱敏形态（T361 三态 = masked）
	e.store.updatedTech = &repo.TechnicianRow{
		TechID: "T1", Name: "旧名", PhoneEnc: newEnc, PhoneHash: phone.Hash(t625NewPhone),
		TeamID: strPtr("TEAM01"), Status: "enabled", AuthStatus: "authorized",
	}
	w, resp = e.do(http.MethodPut, "/api/v1/admin/technicians/T1",
		map[string]any{"phone": t625NewPhone}, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var dto model.TechnicianDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "137****3333", dto.PhoneMasked, "响应只出脱敏串")
	assert.Equal(t, string(phone.PhoneStateMasked), dto.PhoneState)
	assert.NotContains(t, w.Body.String(), t625NewPhone, "明文号码不得出现在响应体任何位置")
	assert.Equal(t, "T1", dto.TechID)
}

// ─────────────────────────────────────────────────────────────
// 4) 重复 phone → 409（handler.go:1237），且零写库
// ─────────────────────────────────────────────────────────────

func TestT625_UpdateTechnician_DuplicatePhoneIsConflictWithNoWrite(t *testing.T) {
	e, _ := t625TechEnv(t)
	e.store.phoneTaken = true
	sentinelName := "SENTINEL-UNTouched"
	e.store.lastTechInput = repo.TechInput{Name: sentinelName}

	w, resp := e.do(http.MethodPut, "/api/v1/admin/technicians/T1",
		map[string]any{"name": "抢别人号", "phone": t625NewPhone}, nil)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, model.CodeConflict, resp.Code)

	// 判重即止：UpdateTechnician 未被调用（入参哨兵字段原封不动）
	assert.Equal(t, sentinelName, e.store.lastTechInput.Name, "409 之后不得有写库动作")
	assert.Empty(t, e.store.lastTechInput.PhoneEnc)
	assert.Empty(t, e.store.lastTechInput.PhoneHash)
}

// ─────────────────────────────────────────────────────────────
// 5) 新建技师不产生任何角色/权限关联（现可断言的形：technicians 无 role 列、DTO/TechInput 无角色字段、角色侧写零）
// ─────────────────────────────────────────────────────────────

// 现读事实（不是期望，是「类 5 的可观测面」）：
//   - scripts/db/migrations/000001_init_schema.up.sql:53-65 technicians 无 role/权限列（roles 表 :18-22 与之无外键、无中间表）
//   - services/user-service/internal/repo/pg.go:788 INSERT 只写 tech_id,name,phone_enc,phone_hash,team_id,password_hash
//   - services/user-service/internal/handler/handler.go:1081-1085 techRequest 只有 name/phone/teamId
//   - teamId 可选且只做存在性校验（validateTechTeam :1119 → TeamExists），不校验团队语义
func TestT625_CreateTechnician_ProducesNoRoleOrPermissionLink(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.createdTech = &repo.TechnicianRow{
		TechID: "TECH-NEW", Name: "新技师", TeamID: strPtr("TEAM01"),
		Status: "enabled", AuthStatus: "authorized",
	}
	// 库里存在可绑的角色行（若实现真想挂角色，这里有现成目标）
	e.store.roles = []repo.RoleRow{{RoleID: "ROLE_TECH", Name: "技师"}}

	// 载荷里主动塞角色键：证明它们既不被消费也不被回显
	w, resp := e.do(http.MethodPost, "/api/v1/admin/technicians", map[string]any{
		"name": "新技师", "phone": "13800004444", "teamId": "TEAM01",
		"role": "ROLE_TECH", "roleId": "ROLE_TECH", "roleIds": []string{"ROLE_TECH"},
		"permissions": []string{"*"},
	}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	// 落库入参 = TechInput 全部字段，不含任何角色/权限语义
	in := e.store.lastTechInput
	assert.Equal(t, "新技师", in.Name)
	assert.Equal(t, "TEAM01", *in.TeamID)
	assert.NotEmpty(t, in.PasswordHash, "T480 初始口令仍在写入面内")
	for _, field := range reflectWritableFields(repo.TechInput{}) {
		lower := strings.ToLower(field)
		for _, forbidden := range []string{"role", "permission", "auth", "scope"} {
			assert.NotContains(t, lower, forbidden,
				"repo.TechInput 不得出现角色/权限字段（实际字段 %s）", field)
		}
	}

	// 响应体同样没有角色面：DTO 字段名/JSON 键里都不含 role/permission
	var dto model.TechnicianCreateDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	for _, field := range reflectJSONKeys(model.TechnicianCreateDTO{}) {
		lower := strings.ToLower(field)
		for _, forbidden := range []string{"role", "permission"} {
			assert.NotContains(t, lower, forbidden,
				"新建技师响应不得出现角色字段（实际键 %s）", field)
		}
	}
	body := w.Body.String()
	assert.NotContains(t, body, "ROLE_TECH", "自报角色不得被原样回显")
	assert.NotContains(t, strings.ToLower(body), "roleid")

	// 角色侧存储零动作：既没建角色也没改权限矩阵
	assert.Empty(t, e.store.lastRoleName, "CreateTechnician 不得触达 CreateRole")
	assert.Empty(t, e.store.lastRolePerms)
	assert.Empty(t, e.store.lastPermJSON, "不得触达 UpdateRolePermissions")
	assert.Nil(t, e.store.roleUpdName, "不得触达 UpdateRole")
	assert.False(t, e.store.resetCalled, "不得顺带改告警规则配置")
}

// reflectWritableFields 取结构体字段名列表（不依赖反射改库，只为「无角色字段」这条判据服务）
func reflectWritableFields(sample repo.TechInput) []string {
	typ := reflect.TypeOf(sample)
	names := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		names = append(names, typ.Field(i).Name)
	}
	return names
}

// reflectJSONKeys 取结构体（含内嵌）序列化后的 JSON 键集合
func reflectJSONKeys(sample model.TechnicianCreateDTO) []string {
	typ := reflect.TypeOf(sample)
	keys := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			inner := reflect.New(field.Type).Elem()
			for _, k := range reflectJSONKeysOf(inner.Interface()) {
				keys = append(keys, k)
			}
			continue
		}
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag == "" {
			tag = field.Name
		}
		keys = append(keys, tag)
	}
	return keys
}

func reflectJSONKeysOf(anyStruct any) []string {
	typ := reflect.TypeOf(anyStruct)
	keys := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag == "" {
			tag = typ.Field(i).Name
		}
		keys = append(keys, tag)
	}
	return keys
}

// ─────────────────────────────────────────────────────────────
// 6) T623 产品要求：新建技师的团队只能从「维护人员团队」里选 —— PRD 未定稿，不钉断言
// ─────────────────────────────────────────────────────────────

func TestT625_CreateTechnician_TeamMustBeMaintenanceOnly(t *testing.T) {
	reason := "T623 未合入：PRD 尚未定稿「技师角色 = 维护人员团队」的归类口径" +
		"（teams 表现无团队类型列，scripts/db/migrations/000001_init_schema.up.sql:10-16；" +
		"validateTechTeam handler.go:1119 只验存在性）——PRD 定稿前不写死断言，合入后去掉本行即转绿"
	t.Skip(reason)

	// 定稿后要补的断言形状（此处仅记录，不生效）：
	//   1. 非维护类 teamId → 400；
	//   2. teams 表新增类型列后，新建技师的 teamId 必须落在该类型子集；
	//   3. 角色若由 team 推导，响应回显 role 口径与 PRD 术语一致。
	e := newEnv(t, true, true)
	e.store.teamExists = true
	w, _ := e.do(http.MethodPost, "/api/v1/admin/technicians", map[string]any{
		"name": "归类测试", "phone": "13800005555", "teamId": "TEAM-DOCTOR",
	}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, "非维护人员团队应被拒（现实现放行）")
}
