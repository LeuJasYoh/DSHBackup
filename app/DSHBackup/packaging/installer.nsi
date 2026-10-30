Unicode true
SetCompressor /SOLID lzma
SetCompressorDictSize 32
SetDatablockOptimize on
CRCCheck on
ManifestDPIAware true
RequestExecutionLevel user

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "StrFunc.nsh"

${StrStr}
${UnStrStr}

!ifndef VERSION
  !define VERSION "1.3.1"
!endif
!ifndef VERSION4
  !define VERSION4 "1.3.1.0"
!endif
!ifndef SOURCE
  !define SOURCE "..\..\..\dist\DSHBackup"
!endif
!ifndef OUT
  !define OUT "..\..\..\dist\DSHBackup-${VERSION}-win64-setup.exe"
!endif
!ifndef ICON
  !define ICON "..\build\windows\icon.ico"
!endif
!ifndef WIZARD
  !define WIZARD "${__FILEDIR__}\installer-wizard.bmp"
!endif

Name "DSH Backup ${VERSION}"
OutFile "${OUT}"
InstallDir "$LOCALAPPDATA\Programs\DSHBackup"
InstallDirRegKey HKCU "Software\DSHBackup" "InstallDir"
BrandingText "DSH Backup ${VERSION}"

VIProductVersion "${VERSION4}"
VIAddVersionKey /LANG=2052 "ProductName" "DSH Backup"
VIAddVersionKey /LANG=2052 "FileDescription" "DSH Backup 安装程序"
VIAddVersionKey /LANG=2052 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=2052 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=2052 "CompanyName" "DSH Backup"
VIAddVersionKey /LANG=2052 "LegalCopyright" "Copyright 2026"

!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\DSHBackup"
!define APP_KEY "Software\DSHBackup"

Var Running
Var InvalidReason

!define MUI_ABORTWARNING
!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_WELCOMEFINISHPAGE_BITMAP "${WIZARD}"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "${WIZARD}"

!define MUI_WELCOMEPAGE_TITLE "安装 DSH Backup ${VERSION}"
!define MUI_WELCOMEPAGE_TEXT "DSH Backup 只在本机备份 DSH 的核心数据，配置、密钥和备份都不会上传。$\r$\n$\r$\n安装版会把程序、配置、密钥和本地备份都放在安装目录。请选择一个当前用户可写的文件夹。$\r$\n$\r$\n安装程序还会安装 Rust 核心与官方 7-Zip CLI，并可选择创建桌面快捷方式。"
!insertmacro MUI_PAGE_WELCOME

!define MUI_DIRECTORYPAGE_TEXT_TOP "DSH Backup 会把配置、密钥和本地备份保存在安装目录，因此不能安装到 C:\Program Files 等受保护目录。默认位置在当前用户的程序目录，你也可以选择其他可写文件夹。"
!define MUI_DIRECTORYPAGE_TEXT_DESTINATION "安装位置"
!define MUI_PAGE_CUSTOMFUNCTION_LEAVE ValidateDirectory
!insertmacro MUI_PAGE_DIRECTORY

!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES

!define MUI_FINISHPAGE_RUN "$INSTDIR\DSHBackup.exe"
!define MUI_FINISHPAGE_RUN_TEXT "立即运行 DSH Backup"
!define MUI_FINISHPAGE_TEXT "DSH Backup 已安装到：$\r$\n$INSTDIR$\r$\n$\r$\n配置、密钥和本地备份也会保存在这里。卸载时安装程序会保留 data 和 backups 文件夹。"
!insertmacro MUI_PAGE_FINISH

!define MUI_UNCONFIRMPAGE_TEXT_TOP "卸载会删除 DSH Backup 的程序文件，但会保留安装目录中的 data（配置、密钥）和 backups（本地备份）。如需彻底删除，请在卸载后手动删除整个安装目录。"
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!define MUI_UNFINISHPAGE_TEXT "DSH Backup 的程序文件已删除。$\r$\n$\r$\n安装目录中的 data 和 backups（如果存在）已保留。如需彻底删除，请手动删除该目录。"
!insertmacro MUI_UNPAGE_FINISH

!insertmacro MUI_LANGUAGE "SimpChinese"

LangString DESC_SEC_CORE ${LANG_SIMPCHINESE} "DSH Backup 主程序、Rust 核心、7-Zip CLI、说明与许可证。"
LangString DESC_SEC_DESKTOP ${LANG_SIMPCHINESE} "在桌面创建 DSH Backup 快捷方式。"

Section "DSH Backup 程序文件" SEC_CORE
  SectionIn RO
  SetOutPath "$INSTDIR"
  SetOverwrite on
  File "/oname=DSHBackup.exe" "${SOURCE}\DSHBackup.exe"
  File "/oname=dsh-backup-core.exe" "${SOURCE}\dsh-backup-core.exe"
  File "/oname=icon.ico" "${SOURCE}\icon.ico"
  File "/oname=README.txt" "${SOURCE}\README.txt"
  File "/oname=THIRD_PARTY_NOTICES.txt" "${SOURCE}\THIRD_PARTY_NOTICES.txt"
  File "/oname=7-Zip-SOURCE.txt" "${SOURCE}\7-Zip-SOURCE.txt"
  File "/oname=BUILD.txt" "${SOURCE}\BUILD.txt"
  SetOutPath "$INSTDIR\tools"
  File "/oname=7za.exe" "${SOURCE}\tools\7za.exe"
  File "/oname=License.txt" "${SOURCE}\tools\License.txt"
  SetOutPath "$INSTDIR"

  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateDirectory "$SMPROGRAMS\DSH Backup"
  CreateShortCut "$SMPROGRAMS\DSH Backup\DSH Backup.lnk" "$INSTDIR\DSHBackup.exe" "" "$INSTDIR\DSHBackup.exe" 0 SW_SHOWNORMAL "" "DSH Backup"
  CreateShortCut "$SMPROGRAMS\DSH Backup\卸载 DSH Backup.lnk" "$INSTDIR\uninstall.exe" "" "$INSTDIR\uninstall.exe" 0

  WriteRegStr HKCU "${APP_KEY}" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "DSH Backup"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "DSH Backup"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\DSHBackup.exe,0"
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegStr HKCU "${UNINST_KEY}" "URLInfoAbout" "https://github.com/LeuJasYoh/DSHBackup"
  WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" 16000
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
SectionEnd

Section "桌面快捷方式" SEC_DESKTOP
  CreateShortCut "$DESKTOP\DSH Backup.lnk" "$INSTDIR\DSHBackup.exe" "" "$INSTDIR\DSHBackup.exe" 0 SW_SHOWNORMAL "" "DSH Backup"
SectionEnd

Section "-桌面快捷方式同步"
  ${If} ${SectionIsSelected} ${SEC_DESKTOP}
    WriteRegStr HKCU "${APP_KEY}" "DesktopShortcut" "1"
  ${Else}
    Delete "$DESKTOP\DSH Backup.lnk"
    WriteRegStr HKCU "${APP_KEY}" "DesktopShortcut" "0"
  ${EndIf}
SectionEnd

Section "Uninstall"
  Delete "$DESKTOP\DSH Backup.lnk"
  Delete "$SMPROGRAMS\DSH Backup\DSH Backup.lnk"
  Delete "$SMPROGRAMS\DSH Backup\卸载 DSH Backup.lnk"
  RMDir "$SMPROGRAMS\DSH Backup"

  Delete "$INSTDIR\DSHBackup.exe"
  Delete "$INSTDIR\dsh-backup-core.exe"
  Delete "$INSTDIR\icon.ico"
  Delete "$INSTDIR\README.txt"
  Delete "$INSTDIR\THIRD_PARTY_NOTICES.txt"
  Delete "$INSTDIR\7-Zip-SOURCE.txt"
  Delete "$INSTDIR\BUILD.txt"
  Delete "$INSTDIR\.dshbackup-install-probe"
  Delete "$INSTDIR\tools\7za.exe"
  Delete "$INSTDIR\tools\License.txt"
  RMDir "$INSTDIR\tools"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  DeleteRegKey HKCU "${UNINST_KEY}"
  DeleteRegKey HKCU "${APP_KEY}"
SectionEnd

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
!insertmacro MUI_DESCRIPTION_TEXT ${SEC_CORE} $(DESC_SEC_CORE)
!insertmacro MUI_DESCRIPTION_TEXT ${SEC_DESKTOP} $(DESC_SEC_DESKTOP)
!insertmacro MUI_FUNCTION_DESCRIPTION_END

Function NormalizeInstallDir
  ${If} "$INSTDIR" == ""
    Return
  ${EndIf}
  StrLen $0 "$INSTDIR"
  ${If} $0 > 3
    StrCpy $1 "$INSTDIR" 1 -1
    ${If} $1 == "\"
      StrCpy $INSTDIR "$INSTDIR" -1
    ${EndIf}
  ${EndIf}
FunctionEnd

Function CheckProtectedDir
  StrCpy $InvalidReason ""
  ${If} "$INSTDIR" == ""
    StrCpy $InvalidReason "请选择安装位置。"
    Return
  ${EndIf}
  ${GetRoot} "$INSTDIR" $0
  ${If} $0 == "$INSTDIR"
    StrCpy $InvalidReason "不能把 DSH Backup 直接安装到磁盘或共享的根目录。请新建一个文件夹，例如 D:\Apps\DSHBackup。"
    Return
  ${EndIf}
  StrLen $0 "$INSTDIR"
  ${If} $0 == 2
    StrCpy $1 "$INSTDIR" 1 1
    ${If} $1 == ":"
      StrCpy $InvalidReason "不能把 DSH Backup 直接安装到磁盘或共享的根目录。请新建一个文件夹，例如 D:\Apps\DSHBackup。"
      Return
    ${EndIf}
  ${EndIf}
  ${If} $0 == 3
    StrCpy $1 "$INSTDIR" 1 1
    StrCpy $2 "$INSTDIR" 1 2
    ${If} $1 == ":"
      ${If} $2 == "\"
      ${OrIf} $2 == "/"
        StrCpy $InvalidReason "不能把 DSH Backup 直接安装到磁盘或共享的根目录。请新建一个文件夹，例如 D:\Apps\DSHBackup。"
        Return
      ${EndIf}
    ${EndIf}
  ${EndIf}
  ${If} "$INSTDIR" == "$WINDIR"
    StrCpy $InvalidReason "不能安装到 Windows 目录。请选择当前用户可写的文件夹。"
    Return
  ${EndIf}
  StrLen $0 "$WINDIR"
  StrCpy $1 "$INSTDIR" $0
  ${If} $1 == "$WINDIR"
    StrCpy $InvalidReason "不能安装到 Windows 目录。请选择当前用户可写的文件夹。"
    Return
  ${EndIf}
  StrLen $0 "$PROGRAMFILES"
  StrCpy $1 "$INSTDIR" $0
  ${If} $1 == "$PROGRAMFILES"
    StrCpy $InvalidReason "不能安装到 Program Files。DSH Backup 需要把配置、密钥和备份保存在安装目录，请选择当前用户可写的文件夹。"
    Return
  ${EndIf}
  ${If} "$PROGRAMFILES64" != ""
    StrLen $0 "$PROGRAMFILES64"
    StrCpy $1 "$INSTDIR" $0
    ${If} $1 == "$PROGRAMFILES64"
      StrCpy $InvalidReason "不能安装到 Program Files。DSH Backup 需要把配置、密钥和备份保存在安装目录，请选择当前用户可写的文件夹。"
      Return
    ${EndIf}
  ${EndIf}
FunctionEnd

Function ProbeInstallDir
  ClearErrors
  CreateDirectory "$INSTDIR"
  ${If} ${Errors}
    StrCpy $InvalidReason "无法创建安装目录：$INSTDIR$\r$\n请选择一个当前用户可写的文件夹。"
    Return
  ${EndIf}
  ClearErrors
  FileOpen $0 "$INSTDIR\.dshbackup-install-probe" w
  ${If} ${Errors}
    StrCpy $InvalidReason "安装目录不可写：$INSTDIR$\r$\nDSH Backup 需要把配置、密钥和备份保存在这里，请选择其他文件夹。"
    Return
  ${EndIf}
  FileWrite $0 "probe"
  FileClose $0
  Delete "$INSTDIR\.dshbackup-install-probe"
FunctionEnd

Function ValidateDirectory
  Call NormalizeInstallDir
  Call CheckProtectedDir
  ${If} $InvalidReason != ""
    MessageBox MB_ICONEXCLAMATION|MB_OK "$InvalidReason"
    Abort
  ${EndIf}
  Call ProbeInstallDir
  ${If} $InvalidReason != ""
    MessageBox MB_ICONEXCLAMATION|MB_OK "$InvalidReason"
    Abort
  ${EndIf}

  ReadRegStr $0 HKCU "${APP_KEY}" "InstallDir"
  ${If} $0 != ""
    ${If} $0 != "$INSTDIR"
      ${If} ${FileExists} "$0\DSHBackup.exe"
        MessageBox MB_ICONQUESTION|MB_YESNO "检测到旧版本安装在：$\r$\n$0$\r$\n$\r$\n安装到新位置不会迁移 data 和 backups，建议继续安装到原目录。$\r$\n仍要安装到 $INSTDIR 吗？" IDYES +2
        Abort
      ${EndIf}
    ${EndIf}
  ${EndIf}
FunctionEnd

Function ValidateSilentDirectory
  Call NormalizeInstallDir
  Call CheckProtectedDir
  ${If} $InvalidReason != ""
    SetErrorLevel 3
    Quit
  ${EndIf}
  Call ProbeInstallDir
  ${If} $InvalidReason != ""
    SetErrorLevel 3
    Quit
  ${EndIf}
FunctionEnd

Function CheckRunning
  StrCpy $Running "0"
  nsExec::ExecToStack '"$SYSDIR\tasklist.exe" /FI "IMAGENAME eq DSHBackup.exe" /NH /FO CSV'
  Pop $0
  Pop $1
  ${StrStr} $2 $1 "DSHBackup.exe"
  ${If} $2 != ""
    StrCpy $Running "1"
    Return
  ${EndIf}
  nsExec::ExecToStack '"$SYSDIR\tasklist.exe" /FI "IMAGENAME eq dsh-backup-core.exe" /NH /FO CSV'
  Pop $0
  Pop $1
  ${StrStr} $2 $1 "dsh-backup-core.exe"
  ${If} $2 != ""
    StrCpy $Running "1"
  ${EndIf}
FunctionEnd

Function .onInit
  SetShellVarContext current
  ReadRegStr $0 HKCU "${APP_KEY}" "DesktopShortcut"
  ${If} $0 == "0"
    SectionSetFlags ${SEC_DESKTOP} 0
  ${EndIf}

  ${Do}
    Call CheckRunning
    ${If} $Running == "0"
      ${ExitDo}
    ${EndIf}
    ${If} ${Silent}
      SetErrorLevel 1618
      Quit
    ${EndIf}
    MessageBox MB_ICONEXCLAMATION|MB_RETRYCANCEL "DSH Backup 正在运行。请先从托盘退出程序，再点击“重试”。"
  ${Loop}

  ${If} ${Silent}
    Call ValidateSilentDirectory
  ${EndIf}
FunctionEnd

Function un.CheckRunning
  StrCpy $Running "0"
  nsExec::ExecToStack '"$SYSDIR\tasklist.exe" /FI "IMAGENAME eq DSHBackup.exe" /NH /FO CSV'
  Pop $0
  Pop $1
  ${UnStrStr} $2 $1 "DSHBackup.exe"
  ${If} $2 != ""
    StrCpy $Running "1"
    Return
  ${EndIf}
  nsExec::ExecToStack '"$SYSDIR\tasklist.exe" /FI "IMAGENAME eq dsh-backup-core.exe" /NH /FO CSV'
  Pop $0
  Pop $1
  ${UnStrStr} $2 $1 "dsh-backup-core.exe"
  ${If} $2 != ""
    StrCpy $Running "1"
  ${EndIf}
FunctionEnd

Function un.onInit
  SetShellVarContext current
  ${Do}
    Call un.CheckRunning
    ${If} $Running == "0"
      ${ExitDo}
    ${EndIf}
    ${If} ${Silent}
      SetErrorLevel 1618
      Quit
    ${EndIf}
    MessageBox MB_ICONEXCLAMATION|MB_RETRYCANCEL "DSH Backup 正在运行。请先从托盘退出程序，再点击“重试”。"
  ${Loop}
FunctionEnd
