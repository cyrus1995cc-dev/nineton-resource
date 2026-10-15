import { Router } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { requireRole } from '../middleware/auth';
import { bindAssetEmployee, directorySettings, employeeMatches, listEmployees, saveDirectorySettings, syncDirectory } from '../repositories/directory';
import { AppError, boundedText, integer } from '../errors';
export function createDirectoryRouter(db: DatabaseSync) {
  const router = Router();
  router.get('/employees', (req, res) => res.json({ employees: listEmployees(db, String(req.query.search ?? ''), req.user?.role === 'admin' && req.query.all === 'true') }));
  router.use(requireRole('admin'));
  router.get('/status', (_req, res) => res.json({ ...directorySettings(db),
    runs: db.prepare('SELECT * FROM directory_sync_runs ORDER BY id DESC LIMIT 10').all(),
    employeesCount: db.prepare('SELECT COUNT(*) AS n FROM employees').get()?.n,
    departments: db.prepare('SELECT * FROM departments ORDER BY parent_id, dingtalk_id').all(),
    unmappedCount: db.prepare("SELECT COUNT(*) AS n FROM assets WHERE assignee_employee_id IS NULL AND COALESCE(assignee, '') <> ''").get()?.n,
  }));
  router.put('/settings', (req, res) => res.json(saveDirectorySettings(db, req.user!, req.body ?? {})));
  router.post('/sync', async (req, res) => res.json(await syncDirectory(db, req.user)));
  router.patch('/employees/:id', (req, res) => {
    const id = integer(req.params.id, '员工编号'); const reason = boundedText(req.body?.reason, '状态调整原因', 500, true);
    if (typeof req.body?.active !== 'boolean') throw new AppError(400, '员工状态无效');
    if (!db.prepare('SELECT id FROM employees WHERE id = ?').get(id)) throw new AppError(404, '员工不存在');
    if (req.user?.employee_id === id) throw new AppError(409, '不能在当前会话停用本人身份');
    if (!req.body.active && db.prepare("SELECT s.id FROM asset_flow_steps s JOIN users u ON u.id = s.approver_id WHERE u.employee_id = ? AND s.status IN ('pending', 'waiting')").get(id)) throw new AppError(409, '员工仍有待处理审批，请先处理单据');
    db.exec('BEGIN IMMEDIATE');
    try {
      db.prepare('UPDATE employees SET active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(Number(req.body.active), id);
      db.prepare('DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE employee_id = ?)').run(id);
      db.prepare('INSERT INTO audit_logs (actor_id, actor_name, action, target, details_json) VALUES (?, ?, ?, ?, ?)').run(req.user!.id, req.user!.display_name, 'employee_status', String(id), JSON.stringify({ active: req.body.active, reason }));
      db.exec('COMMIT'); res.json({ ok: true });
    } catch (error) { db.exec('ROLLBACK'); throw error; }
  });
  router.get('/mappings', (_req, res) => res.json({ assets: employeeMatches(db) }));
  router.post('/mappings/:code', (req, res) => { bindAssetEmployee(db, req.user!, String(req.params.code), req.body ?? {}); res.json({ ok: true }); });
  return router;
}
