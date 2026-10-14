# SQL 注入登录绕过样例靶场：/login 表单直拼 SQL。
# image: . 上传时由平台 docker build；实例启动注入 FLAG / CHECKPOINT_* 环境变量。
FROM python:3.12-alpine
WORKDIR /app
COPY src/ /app/
EXPOSE 8080
CMD ["python", "server.py"]