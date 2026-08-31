@echo off
setlocal
cd /d "%~dp0"
if not exist "bin\novaly-drama.exe" (
 echo Missing executable. Download and extract the full Windows release package.
 pause
 exit /b 1
)
if not exist "doubao-web-api\bin\doubao-web-api.exe" (
 echo Missing Doubao service. Extract the full package.
 pause
 exit /b 1
)
echo Open http://127.0.0.1:8085 after startup. Keep this window open.
echo Press Ctrl+C to stop. Stop Doubao in Settings before closing this window.
cd backend
"..\bin\novaly-drama.exe"
if errorlevel 1 pause
