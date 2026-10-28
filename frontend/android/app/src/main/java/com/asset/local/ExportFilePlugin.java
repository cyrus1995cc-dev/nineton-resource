package com.asset.local;

import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.util.Base64;
import androidx.activity.result.ActivityResult;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.ActivityCallback;
import com.getcapacitor.annotation.CapacitorPlugin;
import java.io.OutputStream;

@CapacitorPlugin(name = "ExportFile")
public class ExportFilePlugin extends Plugin {
    private volatile boolean saving = false;

    @PluginMethod
    public void save(PluginCall call) {
        String name = call.getString("filename", "资产导出.xlsx");
        String data = call.getString("base64", "");
        if (saving) { call.reject("请先完成当前文件保存"); return; }
        if (!name.endsWith(".xlsx") || data.isEmpty() || data.length() > 24 * 1024 * 1024) {
            call.reject("导出文件无效或过大，请缩小范围后重试"); return;
        }
        saving = true;
        Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet");
        intent.putExtra(Intent.EXTRA_TITLE, name.replaceAll("[\\\\/:*?\"<>|]", "_"));
        try { startActivityForResult(call, intent, "documentCreated"); }
        catch (Exception error) { saving = false; call.reject("无法打开系统保存窗口，请在电脑网页导出"); }
    }

    @ActivityCallback
    private void documentCreated(PluginCall call, ActivityResult result) {
        if (call == null) { saving = false; return; }
        if (result.getResultCode() != Activity.RESULT_OK || result.getData() == null || result.getData().getData() == null) {
            saving = false; JSObject response = new JSObject(); response.put("cancelled", true); call.resolve(response); return;
        }
        Uri uri = result.getData().getData();
        execute(() -> {
            try {
                byte[] bytes = Base64.decode(call.getString("base64", ""), Base64.DEFAULT);
                if (bytes.length == 0 || bytes.length > 16 * 1024 * 1024) throw new IllegalArgumentException();
                try (OutputStream output = getContext().getContentResolver().openOutputStream(uri, "wt")) {
                    if (output == null) throw new java.io.IOException();
                    output.write(bytes);
                }
                call.resolve(new JSObject());
            } catch (Exception error) { call.reject("文件保存失败，请重新导出并选择可写入的位置"); }
            finally { saving = false; }
        });
    }
}
