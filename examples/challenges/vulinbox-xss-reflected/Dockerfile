# 最小可运行靶场：模拟 Vulinbox 风格反射 XSS（端口 8787）。
# 生产环境可替换为真实 vulinbox 镜像；本样例用于验证题目包上传与容器字段落库。
FROM python:3.12-alpine
WORKDIR /app
COPY src/ /app/
EXPOSE 8787
CMD ["python", "server.py"]
