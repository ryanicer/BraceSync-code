/**
 * 技师端患者位显示真源（T443 裁定⑤，Boss 2026-09-28）。
 *
 * 依据：docs/tasks/winner/T446-字段级对读-结论与字段表.md §三 C-5 ——
 * 后端已下发 patientName（alert-service DTO `public.go:96`、`shared-types` interface `Alert`），
 * 改前三处患者位显示的是裸 patientId。这里只做「有姓名显姓名，无姓名回落 ID」，
 * 两形态按各自稿面分流：告警位只显姓名（alerts.html:153），完成页姓名＋ID 同行（complete.html:70-71）。
 *
 * 🔴 空值两族语义不同，别写 ??  ''：alerts 的 patientName 缺失是空串
 * （COALESCE(p.name,'')），InstallRecord／NotificationRecord 那几族是 null。
 * `||` 对两族同时成立。
 */
export function patientDisplayValue(
  name?: string | null,
  id?: string | null,
): string {
  return name || id || '--'
}

/**
 * 完成页患者位（稿面 complete.html:70-71 =「姓名 (ID)」同行）。
 * alerts 稿面那行只有姓名（alerts.html:153），所以这个形态不给告警页用。
 */
export function patientNameWithId(
  name?: string | null,
  id?: string | null,
): string {
  if (name && id) return `${name} (${id})`
  return name || id || '--'
}
