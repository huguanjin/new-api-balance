/**
 * 本地代理服务器 - 解决跨域和DNS问题
 * 
 * 使用方法：
 * 1. 确保已安装 Node.js
 * 2. 在此目录运行：node server.js
 * 3. 访问 http://localhost:3000
 */

const http = require('http');
const https = require('https');
const fs = require('fs');
const path = require('path');
const url = require('url');

const PORT = 3003;
const CONFIG_FILE = path.join(__dirname, 'sites-config.json');

// MIME 类型
const mimeTypes = {
    '.html': 'text/html',
    '.js': 'text/javascript',
    '.css': 'text/css',
    '.json': 'application/json',
    '.png': 'image/png',
    '.jpg': 'image/jpeg',
    '.gif': 'image/gif',
    '.ico': 'image/x-icon'
};

const server = http.createServer(async (req, res) => {
    // 设置 CORS 头
    res.setHeader('Access-Control-Allow-Origin', '*');
    res.setHeader('Access-Control-Allow-Methods', 'GET, POST, OPTIONS');
    res.setHeader('Access-Control-Allow-Headers', 'Content-Type, Authorization, New-Api-User');

    // 处理预检请求
    if (req.method === 'OPTIONS') {
        res.writeHead(204);
        res.end();
        return;
    }

    const parsedUrl = url.parse(req.url, true);
    
    // API 代理路由
    if (parsedUrl.pathname === '/api/proxy') {
        await handleProxy(req, res, parsedUrl.query);
        return;
    }

    // 读取配置文件
    if (parsedUrl.pathname === '/api/config' && req.method === 'GET') {
        handleGetConfig(req, res);
        return;
    }

    // 保存配置文件
    if (parsedUrl.pathname === '/api/config' && req.method === 'POST') {
        handleSaveConfig(req, res);
        return;
    }

    // 静态文件服务
    let filePath = parsedUrl.pathname;
    if (filePath === '/') {
        filePath = '/index.html';
    }

    const fullPath = path.join(__dirname, filePath);
    const ext = path.extname(fullPath);
    const contentType = mimeTypes[ext] || 'text/plain';

    fs.readFile(fullPath, (err, data) => {
        if (err) {
            if (err.code === 'ENOENT') {
                res.writeHead(404);
                res.end('404 Not Found');
            } else {
                res.writeHead(500);
                res.end('Internal Server Error');
            }
            return;
        }

        res.writeHead(200, { 'Content-Type': contentType + '; charset=utf-8' });
        res.end(data);
    });
});

// 代理请求处理
async function handleProxy(req, res, query) {
    const targetUrl = query.url;
    
    if (!targetUrl) {
        res.writeHead(400, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ success: false, message: '缺少 url 参数' }));
        return;
    }

    try {
        const parsed = new URL(targetUrl);
        const isHttps = parsed.protocol === 'https:';
        const httpModule = isHttps ? https : http;

        // 获取请求头 - 确保原样传递，不做任何处理
        const headers = {
            'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
            'Accept': 'application/json',
            'Host': parsed.host  // 添加 Host 头，某些服务器需要
        };

        // 转发 Authorization 和 New-Api-User 头（原样传递）
        if (req.headers['authorization']) {
            headers['Authorization'] = req.headers['authorization'];
        }
        if (req.headers['new-api-user']) {
            headers['New-Api-User'] = req.headers['new-api-user'];
        }

        const options = {
            hostname: parsed.hostname,
            port: parsed.port || (isHttps ? 443 : 80),
            path: parsed.pathname + (parsed.search || ''),
            method: req.method,
            headers: headers,
            timeout: 15000
        };

        console.log(`[Proxy] ${req.method} ${targetUrl}`);
        console.log(`[Proxy] Headers:`, JSON.stringify({
            Authorization: headers['Authorization'] ? headers['Authorization'].substring(0, 20) + '...' : 'none',
            'New-Api-User': headers['New-Api-User'] || 'none'
        }));

        const proxyReq = httpModule.request(options, (proxyRes) => {
            let data = '';

            proxyRes.on('data', (chunk) => {
                data += chunk;
            });

            proxyRes.on('end', () => {
                console.log(`[Proxy] Response Status: ${proxyRes.statusCode}`);
                
                // 检查返回的是否是 JSON
                const isJson = data.trim().startsWith('{') || data.trim().startsWith('[');
                
                // 如果目标站点返回了错误状态码或非 JSON 响应
                if (proxyRes.statusCode >= 400 || !isJson) {
                    console.log(`[Proxy] 非JSON响应或错误: ${data.substring(0, 200)}`);
                    res.writeHead(200, {
                        'Content-Type': 'application/json; charset=utf-8',
                        'Access-Control-Allow-Origin': '*'
                    });
                    res.end(JSON.stringify({ 
                        success: false, 
                        message: proxyRes.statusCode >= 400 
                            ? `目标站点返回 ${proxyRes.statusCode} 错误` 
                            : '目标站点返回了非JSON响应，可能是网关错误或认证失败'
                    }));
                    return;
                }
                
                res.writeHead(proxyRes.statusCode, {
                    'Content-Type': 'application/json; charset=utf-8',
                    'Access-Control-Allow-Origin': '*'
                });
                res.end(data);
                console.log(`[Proxy] 成功返回 JSON 数据`);
            });
        });

        proxyReq.on('error', (err) => {
            console.error(`[Proxy Error] ${err.message}`);
            // 检查响应是否已发送，避免重复写入
            if (!res.headersSent) {
                res.writeHead(500, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ 
                    success: false, 
                    message: `代理请求失败: ${err.message}`,
                    error: err.code
                }));
            }
        });

        proxyReq.on('timeout', () => {
            proxyReq.destroy();
            // 检查响应是否已发送
            if (!res.headersSent) {
                res.writeHead(504, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ 
                    success: false, 
                    message: '请求超时' 
                }));
            }
        });

        proxyReq.end();

    } catch (err) {
        console.error(`[Error] ${err.message}`);
        // 检查响应是否已发送
        if (!res.headersSent) {
            res.writeHead(500, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ 
                success: false, 
                message: err.message 
            }));
        }
    }
}

server.listen(PORT, () => {
    console.log('========================================');
    console.log('  多站点余额查询 - 本地代理服务器');
    console.log('========================================');
    console.log(`  访问地址: http://localhost:${PORT}`);
    console.log(`  配置文件: ${CONFIG_FILE}`);
    console.log('  按 Ctrl+C 停止服务器');
    console.log('========================================');
});

// 读取配置文件
function handleGetConfig(req, res) {
    try {
        if (fs.existsSync(CONFIG_FILE)) {
            const data = fs.readFileSync(CONFIG_FILE, 'utf-8');
            res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
            res.end(JSON.stringify({ success: true, data: JSON.parse(data) }));
            console.log('[Config] 读取配置成功');
        } else {
            res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
            res.end(JSON.stringify({ success: true, data: [] }));
            console.log('[Config] 配置文件不存在，返回空数组');
        }
    } catch (err) {
        console.error('[Config Error]', err.message);
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ success: false, message: err.message }));
    }
}

// 保存配置文件
function handleSaveConfig(req, res) {
    let body = '';
    
    req.on('data', chunk => {
        body += chunk.toString();
    });
    
    req.on('end', () => {
        try {
            const data = JSON.parse(body);
            fs.writeFileSync(CONFIG_FILE, JSON.stringify(data, null, 2), 'utf-8');
            res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
            res.end(JSON.stringify({ success: true, message: '保存成功' }));
            console.log('[Config] 配置已保存到文件');
        } catch (err) {
            console.error('[Config Error]', err.message);
            res.writeHead(500, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ success: false, message: err.message }));
        }
    });
}
