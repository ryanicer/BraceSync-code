// T314 医护账号写通道：handler 单测的 fake 状态（由 handler_impl_test.go 的 docAcct 字段持有）
//
// 口径同 flow_fake_test.go：这里只做「返回值 / 错误注入 / 入参记录」，
// 双表事务、发号序列、唯一键重发由 repo/doctor_accounts_t314_it_test.go 的
// testcontainers 集成用例覆盖（CI 跑）。
package handler

import (
	"context"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

type fakeDoctorAccountState struct {
	created    *repo.DoctorRow
	createErr  error
	lastCreate repo.DoctorAccountInput

	updated      *repo.DoctorRow
	updateErr    error
	lastUpdateID string
	lastUpdate   repo.DoctorAccountUpdate

	statusRow  *repo.DoctorRow
	statusErr  error
	lastStatus string
	lastStatID string
	pwRow      *repo.DoctorRow
	pwErr      error
	lastPwID   string
	lastPwHash string
}

// echoPhone 把入参手机号密文回填到 stub 行上——真 repo 在写入后立刻回读同一行，
// 出参带的就是刚落库的密文；fake 不回填会让 handler 的脱敏分支测不到东西。
func echoPhone(row *repo.DoctorRow, enc []byte) *repo.DoctorRow {
	if row == nil || len(enc) == 0 {
		return row
	}
	cp := *row
	cp.PhoneEnc = enc
	return &cp
}

func (f *fakeStore) CreateDoctorAccount(_ context.Context, in repo.DoctorAccountInput) (*repo.DoctorRow, error) {
	f.docAcct.lastCreate = in
	if f.docAcct.createErr != nil {
		return nil, f.docAcct.createErr
	}
	return echoPhone(f.docAcct.created, in.PhoneEnc), nil
}

func (f *fakeStore) UpdateDoctorAccount(_ context.Context, doctorID string, in repo.DoctorAccountUpdate) (*repo.DoctorRow, error) {
	f.docAcct.lastUpdateID = doctorID
	f.docAcct.lastUpdate = in
	if f.docAcct.updateErr != nil {
		return nil, f.docAcct.updateErr
	}
	return echoPhone(f.docAcct.updated, in.PhoneEnc), nil
}

func (f *fakeStore) SetDoctorAccountStatus(_ context.Context, doctorID, status string) (*repo.DoctorRow, error) {
	f.docAcct.lastStatID = doctorID
	f.docAcct.lastStatus = status
	if f.docAcct.statusErr != nil {
		return nil, f.docAcct.statusErr
	}
	return f.docAcct.statusRow, nil
}

func (f *fakeStore) SetDoctorAccountPassword(_ context.Context, doctorID, passwordHash string) (*repo.DoctorRow, error) {
	f.docAcct.lastPwID = doctorID
	f.docAcct.lastPwHash = passwordHash
	if f.docAcct.pwErr != nil {
		return nil, f.docAcct.pwErr
	}
	return f.docAcct.pwRow, nil
}
