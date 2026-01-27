// web/js/common.js - 公共JavaScript模块
// 封装管理平台的通用功能，提高代码复用性

// 页面管理器
const PageManager = {
    // 当前页面配置
    currentPage: null,
    
    // 页面配置映射
    pageConfigs: {},
    
    // 初始化页面
    init: function(pageName, config) {
        this.currentPage = pageName;
        this.pageConfigs[pageName] = config;
        
        // 初始化LayUI模块
        layui.use(['element', 'table', 'layer', 'form', 'dropdown'], function() {
            PageManager.layui = {
                element: layui.element,
                table: layui.table,
                layer: layui.layer,
                form: layui.form,
                dropdown: layui.dropdown
            };
            
            // 初始化表格
            PageManager.initTable(config.table);
            
            // 绑定事件
            PageManager.bindEvents(config.events || {});
            
            // 调用页面初始化回调
            if (config.onInit) {
                config.onInit();
            }
        });
    },
    
    // 初始化表格
    initTable: function(tableConfig) {
        if (!tableConfig) return;
        
        // 保存表格实例
        this.tableInstance = this.layui.table.render({
            elem: tableConfig.elem,
            url: tableConfig.url,
            method: tableConfig.method || 'GET',
            page: tableConfig.page !== false,
            cols: [tableConfig.cols],
            response: tableConfig.response || { statusCode: 200 },
            ...(tableConfig.options || {})
        });
        
        // 表格事件由bindEvents函数处理，这里不再重复绑定
        // 表格工具事件会通过handleTableTool函数分发，不需要在这里绑定
        
        // 绑定复选框事件
        if (tableConfig.checkFilter) {
            this.layui.table.on('checkbox(' + tableConfig.checkFilter + ')', function(obj) {
                PageManager.handleTableCheckbox(obj);
            });
        }
    },
    
    // 绑定事件
    bindEvents: function(events) {
        if (!events) return;
        
        // 遍历所有事件配置
        Object.keys(events).forEach(function(eventKey) {
            var eventHandler = events[eventKey];
            
            // 处理表单提交事件
            if (eventKey.startsWith('submit(') && eventKey.endsWith(')')) {
                var filter = eventKey.substring(7, eventKey.length - 1);
                PageManager.layui.form.on(eventKey, eventHandler);
            }
            // 处理表格工具事件
            else if (eventKey.startsWith('tool(') && eventKey.endsWith(')')) {
                // 绑定表格工具事件
                PageManager.layui.table.on(eventKey, function(obj) {
                    PageManager.handleTableTool(obj);
                });
            }
            // 处理其他事件
            else {
                // 支持其他类型的事件绑定
                var eventParts = eventKey.split('.');
                if (eventParts.length === 2) {
                    var module = eventParts[0];
                    var event = eventParts[1];
                    if (PageManager.layui[module]) {
                        PageManager.layui[module].on(event, eventHandler);
                    }
                }
            }
        });
    },
    
    // 处理表格工具事件
    handleTableTool: function(obj) {
        const config = this.pageConfigs[this.currentPage];
        
        // 支持新的events配置方式
        if (config.events && config.events['tool(' + config.table.toolFilter + ')']) {
            config.events['tool(' + config.table.toolFilter + ')'](obj);
        } 
        // 兼容旧的toolEvents配置方式
        else if (config.table && config.table.toolEvents && config.table.toolEvents[obj.event]) {
            config.table.toolEvents[obj.event](obj);
        }
    },
    
    // 处理表格复选框事件
    handleTableCheckbox: function(obj) {
        const config = this.pageConfigs[this.currentPage];
        const checkStatus = this.layui.table.checkStatus(obj.config.id);
        const checkedCount = checkStatus.data.length;
        
        // 更新按钮状态
        parent.$('#edit-btn').prop('disabled', checkedCount !== 1);
        parent.$('#delete-btn').prop('disabled', checkedCount < 1);
        
        // 调用自定义复选框事件
        if (config.table && config.table.onCheckbox) {
            config.table.onCheckbox(checkStatus);
        }
    },
    
    // 刷新表格
    refreshTable: function() {
        if (this.tableInstance) {
            this.tableInstance.reload();
        }
    },
    
    // 显示消息
    showMessage: function(msg, icon = 0) {
        this.layui.layer.msg(msg, { icon: icon });
    },
    
    // 显示确认弹窗
    confirm: function(msg, callback, cancelCallback) {
        this.layui.layer.confirm(msg, function(index) {
            if (callback) callback();
            this.layui.layer.close(index);
        }.bind(this));
    }
};

// Web终端管理器
const TerminalManager = {
    // 终端实例
    currentTerminal: null,
    // WebSocket连接
    currentWebSocket: null,
    
    // 初始化终端
    init: function(container, device, loginData, options = {}) {
        const terminalDiv = document.getElementById(container);
        
        try {
            // 检查xterm.js是否可用
            if (typeof Terminal === 'undefined') {
                throw new Error('xterm.js 库未正确加载');
            }
            
            // 创建xterm.js终端实例
            this.currentTerminal = new Terminal({
                cursorBlink: true,
                cursorStyle: 'block',
                scrollback: 1000,
                fontSize: 14,
                fontFamily: 'Consolas, Monaco, "Courier New", monospace',
                theme: {
                    background: '#000000',
                    foreground: '#f0f0f0',
                    cursor: '#ffffff',
                    cursorAccent: '#000000',
                    selection: '#4a4a4a',
                    black: '#000000',
                    red: '#ff4444',
                    green: '#99cc99',
                    yellow: '#ffcc66',
                    blue: '#6699cc',
                    magenta: '#cc99cc',
                    cyan: '#66cccc',
                    white: '#f0f0f0',
                    brightBlack: '#999999',
                    brightRed: '#ff6666',
                    brightGreen: '#aaffaa',
                    brightYellow: '#ffff66',
                    brightBlue: '#99ccff',
                    brightMagenta: '#ff99ff',
                    brightCyan: '#66ffff',
                    brightWhite: '#ffffff'
                },
                convertEol: true,
                disableStdin: false,
                allowProposedApi: false,
                windowsMode: navigator.platform.indexOf('Win') > -1,
                rendererType: 'canvas',
                termName: 'xterm-256color',
                lineHeight: 1.0,
                letterSpacing: 0,
                ...options
            });
            
            // 打开终端
            this.currentTerminal.open(terminalDiv);
            this.currentTerminal.focus();
            
            // 显示连接信息
            this.currentTerminal.write('正在连接到 ' + device.ip_address + ':' + loginData.port + '...\r\n');
            
            // 建立WebSocket连接
            this.connectWebSocket(device, loginData);
            
        } catch (error) {
            // 显示错误信息
            terminalDiv.innerHTML = '<div style="color: red; padding: 10px;">终端初始化失败: ' + error.message + '</div>';
        }
    },
    
    // 建立WebSocket连接
    connectWebSocket: function(device, loginData) {
        // 构建WebSocket URL
        const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        let wsURL = wsProtocol + '//' + window.location.host + '/api/v1/devices/' + device.id + '/webterm';
        wsURL += '?username=' + encodeURIComponent(loginData.username);
        wsURL += '&password=' + encodeURIComponent(loginData.password);
        wsURL += '&port=' + loginData.port;
        wsURL += '&cols=' + loginData.cols;
        wsURL += '&rows=' + loginData.rows;
        
        // 建立WebSocket连接
        this.currentWebSocket = new WebSocket(wsURL);
        
        // WebSocket连接打开
        this.currentWebSocket.onopen = () => {
            this.currentTerminal.write('已连接到 ' + device.ip_address + ':' + loginData.port + '\r\n');
            this.currentTerminal.write('WebSocket连接已建立，正在初始化终端...\r\n');
        };
        
        // 接收WebSocket消息
        this.currentWebSocket.onmessage = (event) => {
            if (typeof event.data === 'string') {
                this.currentTerminal.write(event.data);
            } else if (event.data instanceof ArrayBuffer) {
                // 处理二进制消息，转换为字符串
                const decoder = new TextDecoder('utf-8');
                this.currentTerminal.write(decoder.decode(event.data));
            } else if (event.data instanceof Blob) {
                // 处理Blob消息，转换为字符串
                const reader = new FileReader();
                reader.onload = (e) => {
                    this.currentTerminal.write(e.target.result);
                };
                reader.readAsText(event.data);
            }
        };
        
        // WebSocket连接关闭
        this.currentWebSocket.onclose = (event) => {
            this.currentTerminal.write('\r\n');
            this.currentTerminal.write('连接已关闭: ' + event.code + ' - ' + event.reason + '\r\n');
            this.currentTerminal.write('\r\n提示：Agent可能已断开连接，请检查Agent状态或刷新页面重试\r\n');
            this.currentTerminal.write('\r\n按任意键关闭终端...');
            // 禁用输入
            this.currentTerminal.setOption('disableStdin', true);
        };
        
        // WebSocket连接错误
        this.currentWebSocket.onerror = (error) => {
            this.currentTerminal.write('\r\n连接错误: ' + error.message + '\r\n');
            this.currentTerminal.write('\r\n提示：可能是网络问题或服务器暂时不可用，请稍后重试\r\n');
            this.currentTerminal.write('\r\n按任意键关闭终端...');
            // 禁用输入
            this.currentTerminal.setOption('disableStdin', true);
        };
        
        // 处理终端输入
        this.currentTerminal.onData((data) => {
            if (this.currentWebSocket.readyState === WebSocket.OPEN) {
                this.currentWebSocket.send(data);
            }
        });
        
        // 处理终端大小变化
        this.currentTerminal.onResize((size) => {
            if (this.currentWebSocket.readyState === WebSocket.OPEN) {
                // 发送窗口大小调整
                const resizeMsg = JSON.stringify({
                    type: 'resize',
                    cols: size.cols,
                    rows: size.rows
                });
                this.currentWebSocket.send(resizeMsg);
            }
        });
    },
    
    // 关闭终端
    close: function() {
        if (this.currentTerminal) {
            this.currentTerminal.dispose();
            this.currentTerminal = null;
        }
        if (this.currentWebSocket) {
            this.currentWebSocket.close();
            this.currentWebSocket = null;
        }
    }
};

// 工具函数
const Utils = {
    // 发送AJAX请求
    ajax: function(options) {
        return $.ajax({
            contentType: 'application/json',
            dataType: 'json',
            ...options
        });
    },
    
    // 生成唯一ID
    generateId: function() {
        return Date.now().toString(36) + Math.random().toString(36).substr(2);
    },
    
    // 格式化日期
    formatDate: function(date, format = 'YYYY-MM-DD HH:mm:ss') {
        if (!date) return '';
        
        // 简单的日期格式化实现
        const d = new Date(date);
        const year = d.getFullYear();
        const month = String(d.getMonth() + 1).padStart(2, '0');
        const day = String(d.getDate()).padStart(2, '0');
        const hours = String(d.getHours()).padStart(2, '0');
        const minutes = String(d.getMinutes()).padStart(2, '0');
        const seconds = String(d.getSeconds()).padStart(2, '0');
        
        return format
            .replace('YYYY', year)
            .replace('MM', month)
            .replace('DD', day)
            .replace('HH', hours)
            .replace('mm', minutes)
            .replace('ss', seconds);
    }
};

// 暴露全局变量
window.PageManager = PageManager;
window.TerminalManager = TerminalManager;
window.Utils = Utils;
