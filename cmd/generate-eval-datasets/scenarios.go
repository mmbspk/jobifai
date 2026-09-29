package main

import "fmt"

type jobScenario struct {
	Tag      string
	ExpectPass bool
	Profile  map[string]any
	JobDesc  string
	Critical bool
}

func jobScoringScenarios() []jobScenario {
	return []jobScenario{
		{Tag: "strong_exact_fit", ExpectPass: true, Critical: true, Profile: nurseProfile("Registered Nurse", "2019", []string{"acute care", "patient monitoring"}),
			JobDesc: "Registered Nurse for acute care ward. Requires current registration and patient monitoring experience. Melbourne onsite."},
		{Tag: "transferable_fit", ExpectPass: true, Critical: false, Profile: nurseProfile("Enrolled Nurse", "2018", []string{"aged care", "medication administration"}),
			JobDesc: "Registered Nurse role in aged care with medication administration duties. Will consider strong aged-care background."},
		{Tag: "borderline_fit", ExpectPass: true, Critical: false, Profile: nurseProfile("Healthcare Assistant", "2020", []string{"patient transport", "vitals"}),
			JobDesc: "Junior nurse position supporting ward teams. Healthcare assistant experience with vitals preferred."},
		{Tag: "seniority_mismatch", ExpectPass: false, Critical: true, Profile: nurseProfile("Graduate Nurse", "2024", []string{"clinical placement"}),
			JobDesc: "Senior Nurse Unit Manager leading 40-bed ward. Minimum 8 years leadership required."},
		{Tag: "mandatory_skill_mismatch", ExpectPass: false, Critical: true, Profile: nurseProfile("General Nurse", "2017", []string{"med-surg"}),
			JobDesc: "ICU Nurse mandatory critical-care ventilation and arterial line management experience."},
		{Tag: "qualification_mismatch", ExpectPass: false, Critical: true, Profile: nurseProfile("Enrolled Nurse", "2016", []string{"personal care"}),
			JobDesc: "Registered Nurse must hold AHPRA registration before start date."},
		{Tag: "location_mismatch", ExpectPass: false, Critical: false, Profile: nurseProfile("Registered Nurse", "2015", []string{"community nursing"}),
			JobDesc: "Onsite Perth hospital role. Must live within 50km; relocation not offered."},
		{Tag: "insufficient_experience", ExpectPass: false, Critical: true, Profile: nurseProfile("Student Nurse", "2025", []string{"placement"}),
			JobDesc: "Experienced nurse with minimum 5 years post-registration experience required."},
		{Tag: "overqualified", ExpectPass: false, Critical: false, Profile: nurseProfile("Director of Nursing", "2005", []string{"executive leadership", "budgeting"}),
			JobDesc: "Entry-level enrolled nurse assistant supporting daily personal care tasks."},
		{Tag: "title_diff_skills_fit", ExpectPass: true, Critical: false, Profile: nurseProfile("Clinical Care Coordinator", "2014", []string{"ward coordination", "staff rostering"}),
			JobDesc: "Nurse Manager for surgical ward requiring coordination and rostering experience."},
		{Tag: "domain_mismatch", ExpectPass: false, Critical: true, Profile: nurseProfile("Registered Nurse", "2012", []string{"pediatric oncology"}),
			JobDesc: "Commercial litigation paralegal supporting case discovery and document review."},
		{Tag: "tech_mismatch", ExpectPass: false, Critical: false, Profile: pmProfile("Project Manager", "2016", []string{"Agile", "stakeholders"}),
			JobDesc: "SAP S/4HANA implementation lead with ABAP and Fiori customization experience required."},
		{Tag: "strong_exact_fit_pm", ExpectPass: true, Critical: true, Profile: pmProfile("Project Manager", "2013", []string{"construction delivery", "budget control"}),
			JobDesc: "Construction Project Manager for commercial builds with budget control and subcontractor management."},
		{Tag: "work_rights_mismatch", ExpectPass: false, Critical: false, Profile: map[string]any{
			"skills": []string{"accounting", "payroll"}, "experience_details": []map[string]any{{"position": "Accountant", "company": "Ledger Co", "employment_period": "2018 – Present"}},
			"application_defaults": map[string]any{"requires_sponsorship": true, "work_authorization": "requires sponsorship"},
		}, JobDesc: "Accountant role requiring existing unrestricted work rights; sponsorship unavailable."},
		{Tag: "licence_mismatch", ExpectPass: false, Critical: true, Profile: map[string]any{
			"skills": []string{"electrical maintenance"}, "experience_details": []map[string]any{{"position": "Maintenance Technician", "company": "Plant Co", "employment_period": "2017 – Present"}},
		}, JobDesc: "Licensed electrician mandatory; unrestricted electrical licence required."},
	}
}

func nurseProfile(title, since string, skills []string) map[string]any {
	return map[string]any{
		"skills": skills,
		"experience_details": []map[string]any{{
			"position": title, "company": "Regional Health", "employment_period": since + " – Present",
		}},
	}
}

func pmProfile(title, since string, skills []string) map[string]any {
	return map[string]any{
		"skills": skills,
		"experience_details": []map[string]any{{
			"position": title, "company": "BuildRight", "employment_period": since + " – Present",
		}},
	}
}

func synthJobScoring(i int) map[string]any {
	scenarios := jobScoringScenarios()
	s := scenarios[i%len(scenarios)]
	id := fmt.Sprintf("job_scoring-%04d", i+1)
	exp := map[string]any{"pass_threshold": 7, "scenario": s.Tag}
	if s.ExpectPass {
		exp["expect_pass"] = true
		exp["min_score"] = 7
	} else {
		exp["expect_skip"] = true
		exp["max_score"] = 4
	}
	return map[string]any{
		"id": id, "task": "job_scoring", "classification": "synthetic", "critical": s.Critical,
		"tags": []string{s.Tag},
		"input": map[string]any{"profile": s.Profile, "job_description": s.JobDesc},
		"expect": exp,
	}
}

type ethicsScenario struct {
	Title, Company, Description, Verdict string
	Critical                             bool
	Tag                                  string
}

func ethicsScenarios() []ethicsScenario {
	return []ethicsScenario{
		{"Hospital IT Analyst", "Metro Health IT", "Maintain electronic health record systems for patient care units.", "HALAL", true, "hospital_it"},
		{"Primary School Teacher", "Greenfield Education", "Classroom teaching for primary students.", "HALAL", true, "education"},
		{"Warehouse Coordinator", "Pacific Logistics", "Coordinate inbound freight for retail clients.", "DOUBTFUL", false, "mixed_logistics"},
		{"Charity Program Officer", "Relief Foundation", "Manage food distribution programs for underserved communities.", "HALAL", true, "charity"},
		{"Consumer Loan Officer", "City Finance", "Originate personal loans with interest-based repayment schedules.", "HARAM", true, "direct_lending"},
		{"Core Banking Developer", "National Retail Bank", "Build internet banking features including interest-bearing account modules.", "HARAM", true, "bank_software"},
		{"Interest Rate Analyst", "Capital Lending", "Model variable interest rates for corporate lending products.", "HARAM", true, "interest_analyst"},
		{"Distillery Process Operator", "Highlands Distillery", "Operate fermentation lines for alcoholic beverage production.", "HARAM", true, "alcohol_production"},
		{"Brewery IT Administrator", "Harbor Brewery", "Administer production systems for beer manufacturing.", "HARAM", true, "brewery_it"},
		{"Gambling Platform Engineer", "SpinPlay", "Develop online casino wagering platform features.", "HARAM", true, "gambling_dev"},
		{"Casino Accountant", "SpinPlay Finance", "Reconcile wagering revenue and payouts for casino operations.", "HARAM", true, "gambling_accountant"},
		{"Supermarket Cashier", "Everyday Mart", "Checkout operations for general grocery including varied product lines.", "DOUBTFUL", false, "supermarket"},
		{"Pork Processing Technician", "Valley Meats", "Operate pork processing line equipment.", "HARAM", true, "pork_processing"},
		{"Tobacco Packaging Operator", "LeafCo", "Packaging operations for tobacco products.", "HARAM", true, "tobacco"},
		{"Adult Entertainment Editor", "NightMedia", "Edit promotional content for adult entertainment services.", "HARAM", true, "adult_entertainment"},
		{"Weapons Systems Engineer", "ArmsTech", "Design guidance systems for munitions manufacturing.", "HARAM", true, "weapons_direct"},
		{"Defence Logistics Analyst", "SecureMove Defence", "Plan logistics for government defence supply contracts.", "DOUBTFUL", false, "defence_indirect"},
		{"Conglomerate HR Generalist", "GlobalMix Holdings", "HR support across subsidiaries including finance, retail, and media units.", "DOUBTFUL", false, "conglomerate"},
		{"Music Streaming Curator", "WaveTunes", "Curate playlists including mixed lyrical content.", "DOUBTFUL", false, "media_music"},
		{"Role Not Specified", "Unknown Employer", "Details to be discussed.", "DOUBTFUL", false, "insufficient_description"},
		{"Insurance Claims Processor", "Mutual Insurance", "Process general insurance claims including conventional finance-linked products.", "DOUBTFUL", false, "insurance_support"},
		{"Medical Device Technician", "CareTech Devices", "Maintain diagnostic devices used in hospitals.", "HALAL", true, "medical_device"},
		{"Halal Food Quality Auditor", "PureFoods", "Audit halal compliance in food supply chain.", "HALAL", true, "halal_food"},
		{"Real Estate Sales Agent", "Urban Realty", "Sell residential property on commission.", "DOUBTFUL", false, "real_estate"},
		{"Pharmacy Assistant", "Community Pharmacy", "Dispense prescribed medications under pharmacist supervision.", "HALAL", true, "pharmacy"},
		{"Solar Installation Electrician", "SunGrid", "Install rooftop solar systems for homes.", "HALAL", true, "renewable_trades"},
		{"Payroll Officer", "Ledger Partners", "Prepare payroll including deductions; client base includes mixed industries.", "DOUBTFUL", false, "payroll_mixed"},
		{"Airline Cabin Crew", "Regional Air", "Passenger safety and service on commercial flights.", "DOUBTFUL", false, "airline"},
		{"Nonprofit Grants Writer", "Hope Alliance", "Write grant applications for community programs.", "HALAL", true, "nonprofit_grants"},
		{"Private Security Officer", "Shield Services", "Site security for shopping centres.", "HALAL", true, "security"},
	}
}

func synthEmploymentEthics(i int) map[string]any {
	s := ethicsScenarios()[i%len(ethicsScenarios())]
	id := fmt.Sprintf("employment_ethics-%04d", i+1)
	return map[string]any{
		"id": id, "task": "employment_ethics", "classification": "synthetic", "critical": s.Critical,
		"tags": []string{s.Tag},
		"input": map[string]any{"title": s.Title, "company": s.Company, "description": s.Description},
		"expect": map[string]any{"verdict": s.Verdict},
	}
}

type formAnswerScenario struct {
	Tag, Question, Exact string
	Options              []string
	Profile              map[string]any
}

func formAnswerScenarios() []formAnswerScenario {
	return []formAnswerScenario{
		{"work_auth_yes", "Are you authorized to work in Australia?", "Yes", []string{"Yes", "No"}, profileDefaults(false, "4 weeks", "+61400111001", "Sydney")},
		{"sponsorship_no", "Do you require employer sponsorship?", "No", []string{"Yes", "No"}, profileDefaults(false, "4 weeks", "+61400111002", "Melbourne")},
		{"sponsorship_yes", "Do you require employer sponsorship?", "Yes", []string{"Yes", "No"}, profileDefaults(true, "8 weeks", "+61400111003", "Brisbane")},
		{"notice_period", "What is your notice period?", "3 weeks", nil, profileDefaults(false, "3 weeks", "+61400111004", "Perth")},
		{"salary", "Expected annual salary (AUD)?", "95000", nil, mapWithSalary(95000)},
		{"phone", "Best contact phone number?", "+61400111005", nil, profileDefaults(false, "2 weeks", "+61400111005", "Adelaide")},
		{"email", "Email address?", "sam.reed@example.com", nil, mapWithEmail("sam.reed@example.com")},
		{"current_city", "Current city of residence?", "Canberra", nil, profileDefaults(false, "4 weeks", "+61400111006", "Canberra")},
		{"relocation_yes", "Are you willing to relocate?", "Yes", []string{"Yes", "No"}, profileDefaults(false, "4 weeks", "+61400111007", "Hobart")},
		{"years_java", "How many years of Java experience do you have?", "6", nil, mapWithSkillYears("Java", 6)},
		{"years_management", "Years of people management experience?", "4", nil, mapWithMgmtYears(4)},
		{"years_profession", "Years of nursing experience?", "9", nil, mapWithProfessionYears("Registered Nurse", 9)},
		{"licence", "Do you hold a current driver's licence?", "Yes", []string{"Yes", "No"}, profileDefaults(false, "4 weeks", "+61400111008", "Darwin")},
		{"degree", "Highest completed degree?", "Bachelor of Nursing", nil, mapWithDegree("Bachelor of Nursing")},
		{"certification", "Do you hold CPA certification?", "Yes", []string{"Yes", "No"}, mapWithCert("CPA")},
		{"start_date", "Earliest start date?", "2026-05-01", nil, profileDefaults(false, "4 weeks", "+61400111009", "Sydney")},
		{"remote_pref", "Preferred work arrangement?", "Hybrid", []string{"Remote", "Hybrid", "Onsite"}, profileDefaults(false, "4 weeks", "+61400111010", "Sydney")},
		{"missing_phone", "Preferred phone number?", "Not provided in profile", nil, profileDefaults(false, "4 weeks", "", "Sydney")},
		{"hybrid_no", "Are you open to hybrid work?", "No", []string{"Yes", "No"}, profileDefaults(false, "1 month", "+61400111011", "Gold Coast")},
		{"experience_start", "When did you start your current role?", "2019", nil, profileDefaults(false, "4 weeks", "+61400111012", "Newcastle")},
	}
}

func profileDefaults(sponsor bool, notice, phone, city string) map[string]any {
	return map[string]any{
		"application_defaults": map[string]any{"requires_sponsorship": sponsor, "notice_period": notice, "preferred_city": city},
		"personal_information": map[string]any{"phone": phone, "city": city},
		"experience_details":   []map[string]any{{"position": "Coordinator", "employment_period": "2019 – Present"}},
	}
}

func mapWithSalary(aud int) map[string]any {
	p := profileDefaults(false, "4 weeks", "+61400111020", "Sydney")
	p["application_defaults"].(map[string]any)["expected_salary_aud"] = aud
	return p
}

func mapWithEmail(email string) map[string]any {
	p := profileDefaults(false, "4 weeks", "+61400111021", "Sydney")
	p["personal_information"].(map[string]any)["email"] = email
	return p
}

func mapWithSkillYears(skill string, years int) map[string]any {
	p := profileDefaults(false, "4 weeks", "+61400111022", "Sydney")
	p["skills"] = []string{fmt.Sprintf("%s (%d years)", skill, years)}
	return p
}

func mapWithMgmtYears(years int) map[string]any {
	p := profileDefaults(false, "4 weeks", "+61400111023", "Sydney")
	p["experience_details"] = []map[string]any{{"position": "Team Lead", "employment_period": "2018 – Present", "management_years": years}}
	return p
}

func mapWithProfessionYears(title string, years int) map[string]any {
	p := profileDefaults(false, "4 weeks", "+61400111024", "Sydney")
	p["experience_details"] = []map[string]any{{"position": title, "employment_period": fmt.Sprintf("%d – Present", 2026-years)}}
	return p
}

func mapWithDegree(degree string) map[string]any {
	p := profileDefaults(false, "4 weeks", "+61400111025", "Sydney")
	p["education"] = []map[string]any{{"degree": degree}}
	return p
}

func mapWithCert(cert string) map[string]any {
	p := profileDefaults(false, "4 weeks", "+61400111026", "Sydney")
	p["certifications"] = []string{cert}
	return p
}

type appQScenario struct {
	Tag      string
	Profile  map[string]any
	JobCtx   string
	Questions []map[string]string
	Expect   []map[string]any
}

func applicationQuestionScenarios() []appQScenario {
	return []appQScenario{
		{Tag: "auth_sponsor_notice", Profile: appQProfile(true, "8 weeks", 95000, true),
			JobCtx: "Regional logistics company",
			Questions: []map[string]string{
				{"id": "auth", "text": "Are you authorized to work in Australia?"},
				{"id": "sponsor", "text": "Do you require employer sponsorship?"},
				{"id": "notice", "text": "What is your notice period?"},
			},
			Expect: []map[string]any{
				{"question": "Are you authorized to work in Australia?", "match": "exact", "value": "Yes"},
				{"question": "Do you require employer sponsorship?", "match": "exact", "value": "Yes"},
				{"question": "What is your notice period?", "match": "exact", "value": "8 weeks"},
			}},
		{Tag: "years_experience", Profile: appQProfile(false, "4 weeks", 88000, false),
			JobCtx: "Hospital administration",
			Questions: []map[string]string{
				{"id": "years", "text": "How many years of nursing experience do you have?"},
				{"id": "salary", "text": "Expected salary (AUD)?"},
			},
			Expect: []map[string]any{
				{"question": "How many years of nursing experience do you have?", "match": "exact", "value": "7"},
				{"question": "Expected salary (AUD)?", "match": "exact", "value": "88000"},
			}},
		{Tag: "relocation", Profile: appQProfile(false, "4 weeks", 90000, true),
			JobCtx: "National retail HQ",
			Questions: []map[string]string{{"id": "reloc", "text": "Are you willing to relocate to Melbourne?"}},
			Expect: []map[string]any{{"question": "Are you willing to relocate to Melbourne?", "match": "exact", "value": "Yes"}},
		},
		{Tag: "authorization_only", Profile: appQProfile(false, "2 weeks", 72000, false),
			JobCtx: "Community pharmacy",
			Questions: []map[string]string{{"id": "auth", "text": "Do you have unrestricted work rights in Australia?"}},
			Expect: []map[string]any{{"question": "Do you have unrestricted work rights in Australia?", "match": "exact", "value": "Yes"}},
		},
		{Tag: "sponsorship_required", Profile: appQProfile(true, "12 weeks", 105000, false),
			JobCtx: "Construction group",
			Questions: []map[string]string{{"id": "sponsor", "text": "Will you now or in the future require visa sponsorship?"}},
			Expect: []map[string]any{{"question": "Will you now or in the future require visa sponsorship?", "match": "exact", "value": "Yes"}},
		},
		{Tag: "salary_expectation", Profile: appQProfile(false, "4 weeks", 112000, false),
			JobCtx: "Public sector agency",
			Questions: []map[string]string{{"id": "salary", "text": "What are your salary expectations (AUD)?"}},
			Expect: []map[string]any{{"question": "What are your salary expectations (AUD)?", "match": "exact", "value": "112000"}},
		},
		{Tag: "notice_and_start", Profile: appQProfile(false, "6 weeks", 80000, false),
			JobCtx: "Education provider",
			Questions: []map[string]string{
				{"id": "notice", "text": "What is your notice period with your current employer?"},
				{"id": "start", "text": "When can you start?"},
			},
			Expect: []map[string]any{
				{"question": "What is your notice period with your current employer?", "match": "exact", "value": "6 weeks"},
				{"question": "When can you start?", "match": "contains", "contains": "6 weeks"},
			}},
		{Tag: "management_years", Profile: appQProfile(false, "4 weeks", 99000, false),
			JobCtx: "Operations consultancy",
			Questions: []map[string]string{{"id": "mgmt", "text": "How many years have you led a team?"}},
			Expect: []map[string]any{{"question": "How many years have you led a team?", "match": "contains", "contains": "7"}},
		},
	}
}

func appQProfile(sponsor bool, notice string, salary int, relocate bool) map[string]any {
	return map[string]any{
		"personal_information": map[string]any{"name": "Casey", "surname": "Nguyen"},
		"application_defaults": map[string]any{
			"requires_sponsorship": sponsor, "notice_period": notice,
			"expected_salary_aud": salary, "willing_to_relocate": relocate,
			"work_authorization": "authorized",
		},
		"experience_details": []map[string]any{{
			"position": "Registered Nurse", "employment_period": "2019 – Present", "years_experience": 7,
		}},
	}
}

func synthApplicationQuestions(i int) map[string]any {
	scenarios := applicationQuestionScenarios()
	// expand with variants by profession for larger datasets
	base := scenarios[i%len(scenarios)]
	qs := make([]map[string]any, len(base.Questions))
	for j, q := range base.Questions {
		qs[j] = map[string]any{"id": q["id"], "text": q["text"]}
	}
	profile := base.Profile
	id := fmt.Sprintf("application_questions-%04d", i+1)
	return map[string]any{
		"id": id, "task": "application_questions", "classification": "synthetic", "critical": true,
		"tags": []string{base.Tag},
		"input": map[string]any{"profile": profile, "job_context": base.JobCtx, "questions": qs},
		"expect": map[string]any{"answer_count": len(base.Expect), "answers": base.Expect},
	}
}

func synthFormAnswer(i int) map[string]any {
	s := formAnswerScenarios()[i%len(formAnswerScenarios())]
	id := fmt.Sprintf("form_answer-%04d", i+1)
	in := map[string]any{"question": s.Question, "profile_json": s.Profile}
	if len(s.Options) > 0 {
		in["options"] = s.Options
	}
	return map[string]any{
		"id": id, "task": "form_answer", "classification": "synthetic", "tags": []string{s.Tag},
		"input": in, "expect": map[string]any{"exact": s.Exact},
	}
}
