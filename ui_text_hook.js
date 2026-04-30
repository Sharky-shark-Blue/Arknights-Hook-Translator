// ui_text_hook.js - 优化版
// 核心改动：onEnter 只做最轻量的指针读取，所有字符串处理交给异步队列
console.log("[*] UI Text hook loading...");

// ─── 去重配置 ───────────────────────────────────────────────
var DEDUPE_MS   = 1200;
var MAX_CACHE   = 400;          // 降低缓存上限，减少 JS 堆压力
var CACHE_RESET = 3 * 60 * 1000; // 3分钟清一次（原5分钟）

// ─── 去重缓存（只存 plain 文本 → 上次发送时间戳） ───────────
var seenTexts = Object.create(null);
var seenCount = 0;

setInterval(function () {
    seenTexts = Object.create(null);
    seenCount = 0;
}, CACHE_RESET);

// ─── 异步处理队列 ────────────────────────────────────────────
// 把字符串处理从 Unity 主线程剥离，避免阻塞渲染
var pendingQueue = [];
var processingTimer = null;

function scheduleProcess() {
    if (processingTimer !== null) return;
    processingTimer = setTimeout(function () {
        processingTimer = null;
        var batch = pendingQueue;
        pendingQueue = [];
        for (var i = 0; i < batch.length; i++) {
            processBatch(batch[i]);
        }
    }, 0); // 下一个事件循环处理，不阻塞当前调用栈
}

function processBatch(rawPtr) {
    try {
        var s = readIl2CppStringFromPtr(rawPtr);
        if (s) tryPrintText(s);
    } catch (e) {
        // 静默失败，指针已失效时不崩溃
    }
}

// ─── IL2CPP 字符串读取 ────────────────────────────────────────
// 注意：此函数在异步队列里调用，rawPtr 是从 onEnter 里提前复制出来的地址值
function readIl2CppStringFromPtr(strAddr) {
    try {
        var str = ptr(strAddr);
        if (str.isNull()) return "";

        // 尝试偏移 0x10/0x14（Unity 2019~2021 常见布局）
        var s = tryReadUtf16(str, 0x10, 0x14);
        if (s) return s;

        // 尝试偏移 0x14/0x18（Unity 2022+ 布局）
        return tryReadUtf16(str, 0x14, 0x18) || "";
    } catch (e) {
        return "";
    }
}

function tryReadUtf16(str, lenOff, charOff) {
    try {
        var len = str.add(lenOff).readS32();
        if (len <= 0 || len > 1500) return "";
        var s = str.add(charOff).readUtf16String(len);
        if (!s || s.indexOf("\u0000") >= 0) return "";
        return s;
    } catch (e) {
        return "";
    }
}

// ─── 过滤 + 去重 + 发送 ───────────────────────────────────────
var RE_PURE_NUM    = /^\d+$/;
var RE_SLASH_NUM   = /^\/\d+$/;
var RE_NUM_PAIR    = /^\d+\/\d+$/;
var RE_PERCENT     = /^\d{1,3}%$/;
var RE_TIME_JP     = /^\d+[日時間分秒]+$/;
var RE_TIME_DHM    = /^\d+D\d+H\d+M$/i;
var RE_EN_ONLY     = /^[A-Za-z0-9_./:\- ]+$/;
var RE_NUM_SYM     = /^[0-9\s\/:%.\-+]+$/;
var RE_JP_OR_HAN   = /[\u3040-\u30ff\u3400-\u9fff]/;
var RE_RICH_TAG    = /<[^>]+>/g;

function stripRich(s) {
    return s.replace(RE_RICH_TAG, "").trim();
}

function tryPrintText(s) {
    var plain = stripRich(s);
    if (!plain || plain.length < 2 || plain.length > 1200) return;

    if (RE_PURE_NUM.test(plain))  return;
    if (RE_SLASH_NUM.test(plain)) return;
    if (RE_NUM_PAIR.test(plain))  return;
    if (RE_PERCENT.test(plain))   return;
    if (RE_TIME_JP.test(plain))   return;
    if (RE_TIME_DHM.test(plain))  return;
    if (RE_EN_ONLY.test(plain))   return;
    if (RE_NUM_SYM.test(plain))   return;
    if (!RE_JP_OR_HAN.test(plain)) return;

    var now = Date.now();
    var last = seenTexts[plain] || 0;
    if (now - last < DEDUPE_MS) return;

    seenTexts[plain] = now;
    seenCount++;

    // 缓存过大时直接重置，避免 GC 压力积累
    if (seenCount > MAX_CACHE) {
        seenTexts = Object.create(null);
        seenCount = 0;
    }

    // 输出 unicode 转义序列
    var out = "";
    for (var i = 0; i < s.length; i++) {
        var code = s.charCodeAt(i).toString(16);
        while (code.length < 4) code = "0" + code;
        out += "\\u" + code;
    }
    console.log("[unicode] " + out);
}

// ─── IL2CPP API 绑定 ─────────────────────────────────────────
function makeNative(name, ret, args) {
    var p = Module.findExportByName("libil2cpp.so", name);
    return p ? new NativeFunction(p, ret, args) : null;
}

// ─── 主逻辑 ──────────────────────────────────────────────────
function main() {
    if (!Process.findModuleByName("libil2cpp.so")) {
        setTimeout(main, 1000);
        return;
    }

    var il2cpp_domain_get = makeNative("il2cpp_domain_get", "pointer", []);
    var il2cpp_domain_get_assemblies = makeNative("il2cpp_domain_get_assemblies", "pointer", ["pointer", "pointer"]);
    var il2cpp_assembly_get_image = makeNative("il2cpp_assembly_get_image", "pointer", ["pointer"]);
    var il2cpp_image_get_name = makeNative("il2cpp_image_get_name", "pointer", ["pointer"]);
    var il2cpp_class_from_name = makeNative("il2cpp_class_from_name", "pointer", ["pointer", "pointer", "pointer"]);
    var il2cpp_class_get_method_from_name = makeNative("il2cpp_class_get_method_from_name", "pointer", ["pointer", "pointer", "int"]);
    var il2cpp_thread_attach = makeNative("il2cpp_thread_attach", "pointer", ["pointer"]);

    if (!il2cpp_domain_get || !il2cpp_domain_get_assemblies ||
        !il2cpp_assembly_get_image || !il2cpp_image_get_name ||
        !il2cpp_class_from_name || !il2cpp_class_get_method_from_name ||
        !il2cpp_thread_attach) {
        console.log("[!] 缺少必要的 il2cpp 导出函数");
        return;
    }

    var domain = il2cpp_domain_get();
    if (!domain || domain.isNull()) return;

    il2cpp_thread_attach(domain);

    var sizePtr = Memory.alloc(Process.pointerSize);
    var assemblies = il2cpp_domain_get_assemblies(domain, sizePtr);
    if (!assemblies || assemblies.isNull()) return;

    var count = Process.pointerSize === 8
        ? Number(sizePtr.readU64())
        : sizePtr.readU32();

    var imageCache = Object.create(null);

    function findImage(targetName) {
        if (imageCache[targetName]) return imageCache[targetName];

        for (var i = 0; i < count; i++) {
            try {
                var asm = assemblies.add(i * Process.pointerSize).readPointer();
                if (!asm || asm.isNull()) continue;

                var img = il2cpp_assembly_get_image(asm);
                if (!img || img.isNull()) continue;

                var namePtr = il2cpp_image_get_name(img);
                if (!namePtr || namePtr.isNull()) continue;

                if (namePtr.readCString() === targetName) {
                    imageCache[targetName] = img;
                    return img;
                }
            } catch (e) {}
        }
        return null;
    }

    function hookSetText(imageName, ns, className) {
        try {
            var img = findImage(imageName);
            if (!img) return;

            var klass = il2cpp_class_from_name(
                img,
                Memory.allocUtf8String(ns),
                Memory.allocUtf8String(className)
            );
            if (!klass || klass.isNull()) return;

            var method = il2cpp_class_get_method_from_name(
                klass,
                Memory.allocUtf8String("set_text"),
                1
            );
            if (!method || method.isNull()) return;

            var methodPtr = method.readPointer();
            if (!methodPtr || methodPtr.isNull()) return;

            Interceptor.attach(methodPtr, {
                onEnter: function (args) {
                    // ★ 关键优化：onEnter 只读指针值（纯整数操作，极轻量）
                    //   不在这里做任何字符串解析，全部交给异步队列
                    try {
                        var strArg = args[1];
                        if (!strArg || strArg.isNull()) return;

                        // 把指针地址（数字）推入队列，避免持有 NativePointer 对象
                        pendingQueue.push(strArg.toString());
                        scheduleProcess();
                    } catch (e) {}
                }
            });

            console.log("[*] hooked: " + className + ".set_text");
        } catch (e) {
            console.log("[!] hookSetText failed: " + className + " - " + e);
        }
    }

    // UnityEngine.UI.Text
    hookSetText("UnityEngine.UI.dll", "UnityEngine.UI", "Text");

    // TextMeshPro —— 如需开启请取消注释，每多一个 hook 崩溃概率会上升
    // hookSetText("Unity.TextMeshPro.dll", "TMPro", "TMP_Text");
    // hookSetText("Unity.TextMeshPro.dll", "TMPro", "TextMeshProUGUI");

    console.log("[*] UI Text hook install done");
}

setImmediate(main);
