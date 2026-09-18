@echo off
setlocal
set APP_HOME=%~dp0
set DIST_DIR=%APP_HOME%.gradle-dist\gradle-9.6.0
if not exist "%DIST_DIR%\bin\gradle.bat" (
  if not exist "%APP_HOME%.gradle-dist" mkdir "%APP_HOME%.gradle-dist"
  if not exist "%APP_HOME%.gradle-dist\gradle-9.6.0-bin.zip" powershell -NoProfile -ExecutionPolicy Bypass -Command "Invoke-WebRequest -Uri https://services.gradle.org/distributions/gradle-9.6.0-bin.zip -OutFile '%APP_HOME%.gradle-dist\gradle-9.6.0-bin.zip'"
  powershell -NoProfile -ExecutionPolicy Bypass -Command "Expand-Archive -Force '%APP_HOME%.gradle-dist\gradle-9.6.0-bin.zip' '%APP_HOME%.gradle-dist'"
)
call "%DIST_DIR%\bin\gradle.bat" %*
